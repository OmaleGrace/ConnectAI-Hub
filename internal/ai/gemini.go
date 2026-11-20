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

// WithTimeout returns a copy using a dedicated HTTP timeout. It is useful for
// long-running generation tasks without slowing down normal chat requests.
func (c *Client) WithTimeout(timeout time.Duration) *Client {
	if c == nil || timeout <= 0 {
		return c
	}
	clone := *c
	clone.http = &http.Client{Timeout: timeout}
	return &clone
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
	return &Client{apiKey: c.apiKey, model: model, fallback: c.fallback, openRouter: true, http: c.http, eventLogger: c.eventLogger}
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
		return c.openRouterGenerate(ctx, instruction, history, prompt, 2048, 0.4, pdf)
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

// GenerateWithLimits allows larger structured outputs for tasks such as website generation.
func (c *Client) GenerateWithLimits(ctx context.Context, instruction string, history []Message, prompt string, temperature float64, maxOutputTokens int) (string, error) {
	if c.openRouter {
		return c.openRouterGenerate(ctx, instruction, history, prompt, maxOutputTokens, temperature, nil)
	}
	if c.apiKey == "" {
		return "", fmt.Errorf("AI provider is not configured: set OPENROUTER_API_KEY or GEMINI_API_KEY")
	}
	if temperature < 0.1 || temperature > 1.0 {
		temperature = 0.7
	}
	body := requestBody{SystemInstruction: content{Parts: []part{{Text: instruction}}}}
	body.GenerationConfig.Temperature = temperature
	if maxOutputTokens < 256 || maxOutputTokens > 8192 {
		maxOutputTokens = 2048
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
	body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{Text: prompt}}})
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
func (c *Client) openRouterGenerate(ctx context.Context, instruction string, history []Message, prompt string, maxTokens int, temperature float64, pdf []byte) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	type request struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		MaxTokens   int       `json:"max_tokens"`
		Temperature float64   `json:"temperature"`
	}
	type contentPart struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		File *struct {
			Filename string `json:"filename"`
			FileData string `json:"file_data"`
		} `json:"file,omitempty"`
	}
	body := request{Model: c.model, MaxTokens: maxTokens, Temperature: temperature}
	if body.MaxTokens < 256 {
		body.MaxTokens = 2048
	}
	if body.MaxTokens > 8192 {
		body.MaxTokens = 8192
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
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
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
		return "", fmt.Errorf("OpenRouter returned no answer; try again")
	}
	var answer string
	if err := json.Unmarshal(result.Choices[0].Message.Content, &answer); err != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(result.Choices[0].Message.Content, &parts) == nil {
			for _, p := range parts {
				answer += p.Text
			}
		}
	}
	if strings.TrimSpace(answer) == "" {
		return "", fmt.Errorf("OpenRouter returned an empty answer")
	}
	log.Printf("connectai: OpenRouter request succeeded model=%s", c.model)
	if c.eventLogger != nil {
		c.eventLogger("primary_success", c.model, 0)
	}
	return strings.TrimSpace(answer), nil
}
