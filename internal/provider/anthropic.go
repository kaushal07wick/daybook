package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type anthropic struct {
	c  Config
	hc *http.Client
}

func (p *anthropic) Name() string        { return p.c.Name }
func (p *anthropic) Model() string       { return p.c.Model }
func (p *anthropic) Cloud() bool         { return p.c.Cloud }
func (p *anthropic) MaxInputTokens() int { return p.c.MaxInputTokens }

func (p *anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	maxTok := req.MaxTokens
	if maxTok == 0 {
		maxTok = 4096
	}
	body := map[string]any{
		"model":      p.c.Model,
		"max_tokens": maxTok,
		"messages":   []map[string]string{{"role": "user", "content": req.User}},
	}
	sys := req.System
	if req.JSON {
		sys = strings.TrimSpace(sys + "\nRespond with a single JSON object and nothing else.")
	}
	if sys != "" {
		body["system"] = sys
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.c.BaseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("x-api-key", p.c.APIKey)
	hr.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.hc.Do(hr)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode/100 != 2 {
		return Response{}, statusErr(resp, rb)
	}
	var out struct {
		Content []struct{ Type, Text string } `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		return Response{}, fmt.Errorf("anthropic: bad response: %s", rb)
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return Response{Text: sb.String(), InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}, nil
}
