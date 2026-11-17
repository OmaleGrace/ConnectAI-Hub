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
	http        *http.Client
}

// NewClient creates a Gemini client. GEMINI_FALLBACK_MODEL can override the
// default fallback model without changing application code.
func NewClient(apiKey, model string) *Client {
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
	if c.apiKey == "" {
		return "", fmt.Errorf("Gemini is not configured: GEMINI_API_KEY is missing")
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
	if c.apiKey == "" {
		return "", fmt.Errorf("Gemini is not configured: GEMINI_API_KEY is missing")
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
