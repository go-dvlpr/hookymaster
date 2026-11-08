// Package openai is a minimal client for chat completions and embeddings.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const baseURL = "https://api.openai.com/v1"

// Client calls the OpenAI API.
type Client struct {
	apiKey     string
	chatModel  string
	embedModel string
	http       *http.Client
	baseURL    string
}

// New returns a client. The key is never logged.
func New(apiKey, chatModel, embedModel string) *Client {
	return &Client{
		apiKey:     apiKey,
		chatModel:  chatModel,
		embedModel: embedModel,
		http:       &http.Client{Timeout: 120 * time.Second},
		baseURL:    baseURL,
	}
}

// Model is the chat model name, stored with every batch.
func (c *Client) Model() string { return c.chatModel }

// JSON sends a system and a user message and returns the model's JSON object.
func (c *Client) JSON(ctx context.Context, system, user string) ([]byte, error) {
	req := map[string]any{
		"model": c.chatModel,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"response_format": map[string]string{"type": "json_object"},
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := c.post(ctx, "/chat/completions", req, &resp); err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("chat completion: no choices")
	}
	if fr := resp.Choices[0].FinishReason; fr != "stop" {
		return nil, fmt.Errorf("chat completion: finish reason %q", fr)
	}
	return []byte(resp.Choices[0].Message.Content), nil
}

// Embed returns one vector per input text, in input order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	req := map[string]any{"model": c.embedModel, "input": texts}
	var resp struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.post(ctx, "/embeddings", req, &resp); err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d texts", len(resp.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("embeddings: index %d out of range", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", res.StatusCode, truncate(data, 300))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "…"
	}
	return string(b)
}
