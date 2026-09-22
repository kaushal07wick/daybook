// Package provider talks to LLM APIs. Two adapters cover the market:
// OpenAI-compatible chat completions (Ollama, OpenAI, Groq, OpenRouter, …)
// and Anthropic Messages.
package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Request is one prompt. JSON asks the model for a JSON object.
type Request struct {
	System, User string
	JSON         bool
	MaxTokens    int
}

// Response is the model's reply plus usage when reported.
type Response struct {
	Text                      string
	InputTokens, OutputTokens int
}

// Provider is an LLM endpoint.
type Provider interface {
	Name() string
	Model() string
	Cloud() bool
	MaxInputTokens() int
	Complete(ctx context.Context, req Request) (Response, error)
}

// Config is one [providers.<name>] block from config.toml.
type Config struct {
	Name, Type, BaseURL, Model, APIKey string
	Cloud                              bool
	MaxInputTokens, Concurrency        int
	NumCtx                             int // ollama only: context window passed as options.num_ctx
	Timeout                            time.Duration
}

// ErrRateLimited is returned on HTTP 429 so callers can back off.
var ErrRateLimited = errors.New("rate limited")

// New builds a Provider from config.
func New(c Config) (Provider, error) {
	if c.Type == "ollama" {
		if c.NumCtx == 0 {
			c.NumCtx = 16384
		}
		if c.MaxInputTokens == 0 {
			c.MaxInputTokens = c.NumCtx * 3 / 4
		}
	}
	if c.MaxInputTokens == 0 {
		c.MaxInputTokens = 24_000
	}
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Minute
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	client := &http.Client{Timeout: c.Timeout}
	switch c.Type {
	case "openai":
		return &openAI{c: c, hc: client}, nil
	case "ollama":
		return &ollama{c: c, hc: client}, nil
	case "anthropic":
		if c.BaseURL == "" {
			c.BaseURL = "https://api.anthropic.com"
		}
		return &anthropic{c: c, hc: client}, nil
	}
	return nil, fmt.Errorf("provider %q: unknown type %q (want openai|ollama|anthropic)", c.Name, c.Type)
}

// EstimateTokens is a cheap upper-ish bound used for chunking. Agent
// transcripts are shell- and log-heavy and tokenise at ~2.9 chars/token
// (measured on qwen2.5 with Ollama: chars/4 put p90 prompts at 3843 of a
// 4096 context), so chars/3.
// ponytail: swap for a real tokenizer if chunking misfires again.
func EstimateTokens(s string) int { return len(s)/3 + 1 }

func statusErr(resp *http.Response, body []byte) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
