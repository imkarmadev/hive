package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/imkarma/hive/internal/config"
)

// APIRunner calls an LLM provider's HTTP API directly.
type APIRunner struct {
	name   string
	cfg    config.Agent
	apiKey string
	client *http.Client
}

// NewAPIRunner creates a runner that calls LLM APIs.
func NewAPIRunner(name string, cfg config.Agent) (*APIRunner, error) {
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("agent %s: environment variable %s is not set", name, cfg.APIKeyEnv)
	}

	timeout := time.Duration(cfg.DefaultTimeout()) * time.Second

	return &APIRunner{
		name:   name,
		cfg:    cfg,
		apiKey: apiKey,
		client: &http.Client{Timeout: timeout},
	}, nil
}

func (r *APIRunner) Name() string { return r.name }
func (r *APIRunner) Mode() string { return "api" }

// Run sends the prompt to the configured API provider.
func (r *APIRunner) Run(ctx context.Context, req Request) (*Response, error) {
	start := time.Now()

	switch r.cfg.Provider {
	case "openai":
		return r.runOpenAI(ctx, req, start)
	case "anthropic":
		return r.runAnthropic(ctx, req, start)
	case "google":
		return r.runGoogle(ctx, req, start)
	default:
		return nil, fmt.Errorf("unsupported API provider: %s", r.cfg.Provider)
	}
}

// runOpenAI handles OpenAI-compatible APIs (OpenAI, OpenRouter, local proxies).
func (r *APIRunner) runOpenAI(ctx context.Context, req Request, start time.Time) (*Response, error) {
	body := map[string]any{
		"model": r.cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": req.Prompt},
		},
		"max_tokens": 4096,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)

	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return &Response{
			ExitCode: -1,
			Duration: time.Since(start).Seconds(),
			Error:    fmt.Errorf("API call failed: %w", err),
		}, nil
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return &Response{
			Output:   string(respBody),
			ExitCode: httpResp.StatusCode,
			Duration: time.Since(start).Seconds(),
			Error:    fmt.Errorf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		}, nil
	}

	// Parse OpenAI response.
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	output := ""
	if len(result.Choices) > 0 {
		output = result.Choices[0].Message.Content
	}

	return &Response{
		Output:   output,
		ExitCode: 0,
		Duration: time.Since(start).Seconds(),
	}, nil
}

// runAnthropic handles Anthropic's Messages API.
func (r *APIRunner) runAnthropic(ctx context.Context, req Request, start time.Time) (*Response, error) {
	body := map[string]any{
		"model":      r.cfg.Model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": req.Prompt},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", r.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return &Response{
			ExitCode: -1,
			Duration: time.Since(start).Seconds(),
			Error:    fmt.Errorf("API call failed: %w", err),
		}, nil
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return &Response{
			Output:   string(respBody),
			ExitCode: httpResp.StatusCode,
			Duration: time.Since(start).Seconds(),
			Error:    fmt.Errorf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		}, nil
	}

	// Parse Anthropic response.
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	output := ""
	if len(result.Content) > 0 {
		output = result.Content[0].Text
	}

	return &Response{
		Output:   output,
		ExitCode: 0,
		Duration: time.Since(start).Seconds(),
	}, nil
}

// runGoogle handles Google's Generative AI API (Gemini).
func (r *APIRunner) runGoogle(ctx context.Context, req Request, start time.Time) (*Response, error) {
	model := r.cfg.Model
	if model == "" {
		model = "gemini-2.5-pro"
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, r.apiKey)

	body := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]string{
					{"text": req.Prompt},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return &Response{
			ExitCode: -1,
			Duration: time.Since(start).Seconds(),
			Error:    fmt.Errorf("API call failed: %w", err),
		}, nil
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return &Response{
			Output:   string(respBody),
			ExitCode: httpResp.StatusCode,
			Duration: time.Since(start).Seconds(),
			Error:    fmt.Errorf("API returned status %d: %s", httpResp.StatusCode, string(respBody)),
		}, nil
	}

	// Parse Google response.
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	output := ""
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		output = result.Candidates[0].Content.Parts[0].Text
	}

	return &Response{
		Output:   output,
		ExitCode: 0,
		Duration: time.Since(start).Seconds(),
	}, nil
}
