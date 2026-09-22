package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type openAI struct {
	c  Config
	hc *http.Client
}

func (p *openAI) Name() string        { return p.c.Name }
func (p *openAI) Model() string       { return p.c.Model }
func (p *openAI) Cloud() bool         { return p.c.Cloud }
func (p *openAI) MaxInputTokens() int { return p.c.MaxInputTokens }

func (p *openAI) Complete(ctx context.Context, req Request) (Response, error) {
	msgs := []map[string]string{}
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": req.User})
	body := map[string]any{"model": p.c.Model, "messages": msgs, "temperature": 0.2}
	if req.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if p.c.APIKey != "" {
		hr.Header.Set("Authorization", "Bearer "+p.c.APIKey)
	}
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
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rb, &out); err != nil || len(out.Choices) == 0 {
		return Response{}, fmt.Errorf("openai: bad response: %s", rb)
	}
	return Response{Text: out.Choices[0].Message.Content, InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}, nil
}
