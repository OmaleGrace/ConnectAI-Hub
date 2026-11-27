package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type Client struct {
	eventLogger func(eventType, model string, duration time.Duration)
	apiKey      string
	model       string
	fallback    string
	openRouter  bool
	http        *http.Client
}

// NewClient creates a Gemini client. GEMINI_FALLBACK_MODEL can override the
// default fallback model without changing application code.
func NewClient(apiKey, model string) *Client {
	openRouterKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if openRouterKey != "" {
		orModel := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
		if orModel == "" {
			orModel = "openrouter/auto"
		}
		return &Client{
			apiKey:     openRouterKey,
			model:      orModel,
			fallback:   strings.TrimSpace(os.Getenv("OPENROUTER_FALLBACK_MODEL")),
			openRouter: true,
			http:       &http.Client{Timeout: 90 * time.Second},
		}
	}
	if model == "" {
		model = "gemini-3.5-flash"
	}
	fallback := strings.TrimSpace(os.Getenv("GEMINI_FALLBACK_MODEL"))
	if fallback == "" {
		fallback = "gemini-3.5-flash-lite"
	}
	return &Client{
		apiKey:   apiKey,
		model:    model,
		fallback: fallback,
		http:     &http.Client{Timeout: 75 * time.Second},
	}
}

// geminiMaxOutputTokens is the largest answer one Gemini call may return. The default
// (8192) is safe for every Gemini model; raise GEMINI_MAX_OUTPUT_TOKENS (up to 65536)
// if your model supports longer answers.
func geminiMaxOutputTokens() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("GEMINI_MAX_OUTPUT_TOKENS")))
	if err != nil || n < 2048 {
		return 8192
	}
	if n > 65536 {
		return 65536
	}
	return n
}

// Long returns a copy of the client for jobs that legitimately take minutes (building a
// whole website). The normal 75 second client timeout is removed; the caller's context
// is the only time limit, so the caller must always pass one with a deadline.
func (c *Client) Long() *Client {
	if c == nil {
		return c
	}
	cp := *c
	cp.http = &http.Client{}
	return &cp
}

func firstEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// ForModule returns an independent client configuration for one workspace.
// Environment overrides are optional and never mutate the shared client.
func (c *Client) ForModule(module string) *Client {
	if c == nil || !c.openRouter {
		return c
	}
	key := strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(module))
	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL_" + key))
	if model == "" {
		model = c.model
	}
	timeout := 120 * time.Second
	if module == "website" {
		// Website generation can legitimately take longer than chat because it
		// returns several complete frontend files in one response.
		timeout = 420 * time.Second
	}
	return &Client{apiKey: c.apiKey, model: model, fallback: firstEnv("OPENROUTER_FALLBACK_MODEL_"+key, c.fallback), openRouter: true, http: &http.Client{Timeout: timeout}, eventLogger: c.eventLogger}
}

type inlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}
type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inline_data,omitempty"`
}
type content struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}
type requestBody struct {
	SystemInstruction content   `json:"system_instruction"`
	Contents          []content `json:"contents"`
	GenerationConfig  struct {
		Temperature     float64 `json:"temperature"`
		MaxOutputTokens int     `json:"maxOutputTokens"`
	} `json:"generationConfig"`
}
type responseBody struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

type apiFailure struct {
	statusCode int
	status     string
	message    string
}

func (e *apiFailure) Error() string {
	if e.status != "" {
		return fmt.Sprintf("Gemini API error (%s): %s", e.status, e.message)
	}
	return fmt.Sprintf("Gemini API returned HTTP %d: %s", e.statusCode, e.message)
}

func (e *apiFailure) temporary() bool {
	return e.statusCode == http.StatusTooManyRequests || e.statusCode == http.StatusRequestTimeout || e.statusCode >= 500
}

// SetEventLogger attaches a non-blocking-safe observer for operational analytics.
func (c *Client) SetEventLogger(fn func(eventType, model string, duration time.Duration)) {
	c.eventLogger = fn
}
func (c *Client) record(eventType, model string, started time.Time) {
	if c.eventLogger != nil {
		c.eventLogger(eventType, model, time.Since(started))
	}
}

// GenerateWithPDF sends a PDF as Gemini inline data. The caller enforces upload limits.
func (c *Client) GenerateWithPDF(ctx context.Context, instruction string, history []Message, prompt string, pdf []byte) (string, error) {
	if c.openRouter {
		return c.openRouterGenerate(ctx, instruction, history, prompt, 2048, 0.4, pdf, nil)
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("AI provider is not configured: set OPENROUTER_API_KEY or GEMINI_API_KEY")
	}
	body := requestBody{SystemInstruction: content{Parts: []part{{Text: instruction}}}}
	body.GenerationConfig.Temperature = 0.4
	body.GenerationConfig.MaxOutputTokens = 2048
	for _, item := range history {
		if item.Role != "user" && item.Role != "model" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		body.Contents = append(body.Contents, content{Role: item.Role, Parts: []part{{Text: text}}})
	}
	body.Contents = append(body.Contents, content{Role: "user", Parts: []part{
		{Text: prompt},
		{InlineData: &inlineData{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(pdf)}},
	}})
	return c.send(ctx, body)
}

func (c *Client) Generate(ctx context.Context, instruction string, history []Message, prompt string) (string, error) {
	return c.GenerateWithTemperature(ctx, instruction, history, prompt, 0.7)
}

func (c *Client) GenerateWithTemperature(ctx context.Context, instruction string, history []Message, prompt string, temperature float64) (string, error) {
	return c.GenerateWithLimits(ctx, instruction, history, prompt, temperature, 2048)
}

// Image is a picture (for example a website screenshot) sent to a vision-capable model.
type Image struct {
	MimeType string
	Data     []byte
}

// GenerateWithLimits allows larger structured outputs for tasks such as website generation.
func (c *Client) GenerateWithLimits(ctx context.Context, instruction string, history []Message, prompt string, temperature float64, maxOutputTokens int) (string, error) {
	return c.GenerateWithImages(ctx, instruction, history, prompt, temperature, maxOutputTokens, nil)
}

// GenerateWithImages is GenerateWithLimits plus optional images attached to the final user turn.
func (c *Client) GenerateWithImages(ctx context.Context, instruction string, history []Message, prompt string, temperature float64, maxOutputTokens int, images []Image) (string, error) {
	if c.openRouter {
		return c.openRouterGenerate(ctx, instruction, history, prompt, maxOutputTokens, temperature, nil, images)
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("AI provider is not configured: set OPENROUTER_API_KEY or GEMINI_API_KEY")
	}
	if temperature < 0.1 || temperature > 1.0 {
		temperature = 0.7
	}
	body := requestBody{SystemInstruction: content{Parts: []part{{Text: instruction}}}}
	body.GenerationConfig.Temperature = temperature
	if maxOutputTokens < 256 {
		maxOutputTokens = 2048
	}
	if limit := geminiMaxOutputTokens(); maxOutputTokens > limit {
		maxOutputTokens = limit
	}
	body.GenerationConfig.MaxOutputTokens = maxOutputTokens
	for _, item := range history {
		if item.Role != "user" && item.Role != "model" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		body.Contents = append(body.Contents, content{Role: item.Role, Parts: []part{{Text: text}}})
	}
	last := content{Role: "user", Parts: []part{{Text: prompt}}}
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		last.Parts = append(last.Parts, part{InlineData: &inlineData{MimeType: img.MimeType, Data: base64.StdEncoding.EncodeToString(img.Data)}})
	}
	body.Contents = append(body.Contents, last)
	return c.send(ctx, body)
}

func (c *Client) send(ctx context.Context, body requestBody) (string, error) {
	started := time.Now()
	answer, err := c.requestWithRetries(ctx, c.model, body)
	if err == nil {
		log.Printf("connectai: Gemini request succeeded model=%s duration=%s", c.model, time.Since(started).Round(time.Millisecond))
		c.record("primary_success", c.model, started)
		return answer, nil
	}
	if !isTemporary(err) || c.fallback == "" || c.fallback == c.model {
		log.Printf("connectai: Gemini request failed model=%s duration=%s error=%v", c.model, time.Since(started).Round(time.Millisecond), err)
		c.record("failure", c.model, started)
		return "", err
	}

	log.Printf("connectai: primary Gemini model unavailable model=%s; switching to fallback=%s error=%v", c.model, c.fallback, err)
	c.record("fallback_started", c.fallback, started)
	fallbackStarted := time.Now()
	answer, fallbackErr := c.requestWithRetries(ctx, c.fallback, body)
	if fallbackErr != nil {
		log.Printf("connectai: Gemini fallback failed primary=%s fallback=%s duration=%s error=%v", c.model, c.fallback, time.Since(fallbackStarted).Round(time.Millisecond), fallbackErr)
		c.record("failure", c.fallback, fallbackStarted)
		return "", fmt.Errorf("primary model %s failed (%v); fallback model %s also failed: %w", c.model, err, c.fallback, fallbackErr)
	}
	log.Printf("connectai: Gemini fallback succeeded primary=%s fallback=%s duration=%s", c.model, c.fallback, time.Since(fallbackStarted).Round(time.Millisecond))
	c.record("fallback_success", c.fallback, fallbackStarted)
	return answer, nil
}

func (c *Client) requestWithRetries(ctx context.Context, model string, body requestBody) (string, error) {
	const maxAttempts = 3 // initial attempt plus two retries
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		answer, err := c.request(ctx, model, body)
		if err == nil {
			return answer, nil
		}
		lastErr = err
		if !isTemporary(err) || attempt == maxAttempts {
			if isTemporary(err) && attempt == maxAttempts {
				log.Printf("connectai: Gemini retries exhausted model=%s attempts=%d error=%v", model, attempt, err)
			}
			break
		}
		delay := time.Duration(1<<(attempt-1)) * time.Second
		jitter := time.Duration(rand.Int63n(int64(400 * time.Millisecond)))
		log.Printf("connectai: Gemini temporary failure model=%s attempt=%d/%d; retrying in %s error=%v", model, attempt, maxAttempts, delay+jitter, err)
		c.record("retry", model, time.Now())
		timer := time.NewTimer(delay + jitter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", lastErr
}

func isTemporary(err error) bool {
	failure, ok := err.(*apiFailure)
	return ok && failure.temporary()
}

func (c *Client) request(ctx context.Context, model string, body requestBody) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("prepare Gemini request: %w", err)
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create Gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("contact Gemini: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("read Gemini response: %w", err)
	}
	var result responseBody
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("Gemini returned an unreadable response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failure := &apiFailure{statusCode: resp.StatusCode, message: http.StatusText(resp.StatusCode)}
		if result.Error != nil {
			failure.status = result.Error.Status
			if result.Error.Message != "" {
				failure.message = result.Error.Message
			}
		}
		return "", failure
	}
	if len(result.Candidates) == 0 {
		return "", fmt.Errorf("Gemini did not return a response; try rephrasing your message")
	}
	var answer strings.Builder
	for _, item := range result.Candidates[0].Content.Parts {
		answer.WriteString(item.Text)
	}
	if strings.TrimSpace(answer.String()) == "" {
		return "", fmt.Errorf("Gemini returned an empty response")
	}
	return strings.TrimSpace(answer.String()), nil
}

// OpenRouter uses its OpenAI-compatible Chat Completions endpoint. The same
// client is shared by all seven workspaces, keeping provider configuration central.
func (c *Client) openRouterGenerate(ctx context.Context, instruction string, history []Message, prompt string, maxTokens int, temperature float64, pdf []byte, images []Image) (string, error) {
	// An empty assistant message is often a provider/model failure, not a
	// successful response. Retry it and then try the configured fallback.
	answer, err := c.openRouterGenerateOnce(ctx, instruction, history, prompt, maxTokens, temperature, pdf, images)
	if err == nil {
		return answer, nil
	}

	if isOpenRouterRetryable(err) {
		timer := time.NewTimer(1500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
		if answer2, retryErr := c.openRouterGenerateOnce(ctx, instruction, history, prompt, maxTokens, temperature, pdf, images); retryErr == nil {
			log.Printf("connectai: OpenRouter retry succeeded model=%s", c.model)
			return answer2, nil
		} else {
			log.Printf("connectai: OpenRouter retry failed model=%s error=%v", c.model, retryErr)
			err = fmt.Errorf("initial request failed (%v); retry failed: %w", err, retryErr)
		}
	}

	fallback := strings.TrimSpace(c.fallback)
	if fallback != "" && fallback != c.model && isOpenRouterRetryable(err) {
		fallbackClient := *c
		fallbackClient.model = fallback
		log.Printf("connectai: OpenRouter primary failed model=%s; trying fallback=%s", c.model, fallback)
		if answer2, fallbackErr := fallbackClient.openRouterGenerateOnce(ctx, instruction, history, prompt, maxTokens, temperature, pdf, images); fallbackErr == nil {
			log.Printf("connectai: OpenRouter fallback succeeded primary=%s fallback=%s", c.model, fallback)
			return answer2, nil
		} else {
			log.Printf("connectai: OpenRouter fallback failed primary=%s fallback=%s error=%v", c.model, fallback, fallbackErr)
			return "", fmt.Errorf("OpenRouter primary and fallback failed: %v; %w", err, fallbackErr)
		}
	}
	return "", err
}

func isOpenRouterRetryable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"timeout", "deadline", "temporarily unavailable",
		"returned an empty answer", "returned no answer",
		"returned an empty message", "http 408", "http 429",
		"http 500", "http 502", "http 503", "http 504",
		"rate limit", "provider returned",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (c *Client) openRouterGenerateOnce(ctx context.Context, instruction string, history []Message, prompt string, maxTokens int, temperature float64, pdf []byte, images []Image) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	type request struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		MaxTokens   int       `json:"max_tokens"`
		Temperature float64   `json:"temperature"`
		Reasoning   *struct {
			Effort string `json:"effort"`
		} `json:"reasoning,omitempty"`
	}
	type contentPart struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		File *struct {
			Filename string `json:"filename"`
			FileData string `json:"file_data"`
		} `json:"file,omitempty"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url,omitempty"`
	}
	body := request{Model: c.model, MaxTokens: maxTokens, Temperature: temperature}
	if body.MaxTokens < 256 {
		body.MaxTokens = 2048
	}
	if body.MaxTokens > 16384 {
		body.MaxTokens = 16384
	}
	// Reasoning models count their hidden thinking against max_tokens. On large
	// outputs (website generation) that can use the whole budget and leave the
	// answer empty with finish_reason=length, so keep reasoning light there.
	if maxTokens >= 4096 {
		body.Reasoning = &struct {
			Effort string `json:"effort"`
		}{Effort: "low"}
	}
	if body.Temperature < 0.1 || body.Temperature > 1.0 {
		body.Temperature = 0.7
	}
	if strings.TrimSpace(instruction) != "" {
		body.Messages = append(body.Messages, message{Role: "system", Content: instruction})
	}
	for _, item := range history {
		role := item.Role
		if role == "model" {
			role = "assistant"
		}
		if role != "user" && role != "assistant" {
			continue
		}
		if strings.TrimSpace(item.Text) != "" {
			body.Messages = append(body.Messages, message{Role: role, Content: item.Text})
		}
	}
	if len(pdf) > 0 {
		data := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(pdf)
		part := contentPart{Type: "file"}
		part.File = &struct {
			Filename string `json:"filename"`
			FileData string `json:"file_data"`
		}{Filename: "uploaded-document.pdf", FileData: data}
		body.Messages = append(body.Messages, message{Role: "user", Content: []contentPart{{Type: "text", Text: prompt}, part}})
	} else if len(images) > 0 {
		parts := []contentPart{{Type: "text", Text: prompt}}
		for _, img := range images {
			if len(img.Data) == 0 {
				continue
			}
			p := contentPart{Type: "image_url"}
			p.ImageURL = &struct {
				URL string `json:"url"`
			}{URL: "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)}
			parts = append(parts, p)
		}
		body.Messages = append(body.Messages, message{Role: "user", Content: parts})
	} else {
		body.Messages = append(body.Messages, message{Role: "user", Content: prompt})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("prepare OpenRouter request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create OpenRouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", strings.TrimSpace(os.Getenv("APP_BASE_URL")))
	req.Header.Set("X-OpenRouter-Title", "ConnectAI Hub")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("contact OpenRouter: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read OpenRouter response: %w", err)
	}
	var result struct {
		ID       string `json:"id"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Choices  []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   json.RawMessage `json:"content"`
				Reasoning string          `json:"reasoning"`
				Refusal   string          `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("OpenRouter returned an unreadable response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := http.StatusText(resp.StatusCode)
		if result.Error != nil && result.Error.Message != "" {
			msg = result.Error.Message
		}
		return "", fmt.Errorf("OpenRouter API error (HTTP %d): %s", resp.StatusCode, msg)
	}
	if len(result.Choices) == 0 {
		if result.Error != nil && result.Error.Message != "" {
			return "", fmt.Errorf("OpenRouter returned no answer: %s", result.Error.Message)
		}
		return "", fmt.Errorf("OpenRouter returned no answer")
	}

	choice := result.Choices[0]
	var answer strings.Builder
	// Most models return content as a string. Some providers return an array
	// of typed text parts, so support both formats.
	if len(choice.Message.Content) > 0 && string(choice.Message.Content) != "null" {
		if err := json.Unmarshal(choice.Message.Content, new(string)); err == nil {
			var contentText string
			_ = json.Unmarshal(choice.Message.Content, &contentText)
			answer.WriteString(contentText)
		} else {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(choice.Message.Content, &parts) == nil {
				for _, item := range parts {
					if item.Type == "" || item.Type == "text" {
						answer.WriteString(item.Text)
					}
				}
			}
		}
	}
	if strings.TrimSpace(answer.String()) == "" {
		finish := choice.FinishReason
		if finish == "" {
			finish = "not supplied"
		}
		// Do not expose or substitute private reasoning as the assistant answer.
		log.Printf("connectai: OpenRouter empty content model=%s response_model=%s provider=%s finish_reason=%s reasoning_present=%t response_bytes=%d",
			c.model, result.Model, result.Provider, finish, strings.TrimSpace(choice.Message.Reasoning) != "", len(raw))
		return "", fmt.Errorf("OpenRouter returned an empty answer (finish_reason=%s)", finish)
	}
	if result.Usage != nil {
		log.Printf("connectai: OpenRouter request succeeded model=%s response_model=%s provider=%s prompt_tokens=%d completion_tokens=%d total_tokens=%d",
			c.model, result.Model, result.Provider, result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.TotalTokens)
	} else {
		log.Printf("connectai: OpenRouter request succeeded model=%s response_model=%s provider=%s", c.model, result.Model, result.Provider)
	}

	if c.eventLogger != nil {
		c.eventLogger("primary_success", c.model, 0)
	}
	return strings.TrimSpace(answer.String()), nil
}
