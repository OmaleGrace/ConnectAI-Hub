package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewClient(apiKey, model string) *Client {
	if model == "" {
		model = "gemini-3.5-flash"
	}
	return &Client{apiKey: apiKey, model: model, http: &http.Client{Timeout: 75 * time.Second}}
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

// GenerateWithPDF sends a PDF as Gemini inline data. The caller enforces upload limits.
func (c *Client) GenerateWithPDF(ctx context.Context, instruction string, history []Message, prompt string, pdf []byte) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("Gemini is not configured: GEMINI_API_KEY is missing")
	}
	body := requestBody{SystemInstruction: content{Parts: []part{{Text: instruction}}}}
	body.GenerationConfig.Temperature = 0.4
	body.GenerationConfig.MaxOutputTokens = 2048
	for _, item := range history {
		role := item.Role
		if role != "user" && role != "model" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: text}}})
	}
	body.Contents = append(body.Contents, content{Role: "user", Parts: []part{
		{Text: prompt},
		{InlineData: &inlineData{MimeType: "application/pdf", Data: base64.StdEncoding.EncodeToString(pdf)}},
	}})
	return c.send(ctx, body)
}

func (c *Client) Generate(ctx context.Context, instruction string, history []Message, prompt string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("Gemini is not configured: GEMINI_API_KEY is missing")
	}
	body := requestBody{SystemInstruction: content{Parts: []part{{Text: instruction}}}}
	body.GenerationConfig.Temperature = 0.7
	body.GenerationConfig.MaxOutputTokens = 2048
	for _, item := range history {
		role := item.Role
		if role != "user" && role != "model" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: []part{{Text: text}}})
	}
	body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{Text: prompt}}})
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("prepare Gemini request: %w", err)
	}

	url := "https://generativelanguage.googleapis.com/v1beta/models/" + c.model + ":generateContent"
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
		if result.Error != nil && result.Error.Message != "" {
			return "", fmt.Errorf("Gemini API error (%s): %s", result.Error.Status, result.Error.Message)
		}
		return "", fmt.Errorf("Gemini API returned HTTP %d", resp.StatusCode)
	}
	if len(result.Candidates) == 0 {
		return "", fmt.Errorf("Gemini did not return a response; try rephrasing your message")
	}
	var answer strings.Builder
	for _, p := range result.Candidates[0].Content.Parts {
		answer.WriteString(p.Text)
	}
	if strings.TrimSpace(answer.String()) == "" {
		return "", fmt.Errorf("Gemini returned an empty response")
	}
	return strings.TrimSpace(answer.String()), nil
}

func (c *Client) send(ctx context.Context, body requestBody) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("prepare Gemini request: %w", err)
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models/" + c.model + ":generateContent"
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
		if result.Error != nil && result.Error.Message != "" {
			return "", fmt.Errorf("Gemini API error (%s): %s", result.Error.Status, result.Error.Message)
		}
		return "", fmt.Errorf("Gemini API returned HTTP %d", resp.StatusCode)
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
