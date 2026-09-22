package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ollama talks to Ollama's native /api/chat, not its OpenAI-compatible
// shim: it lets us set num_ctx per request, which the OpenAI endpoint has
// no way to express and which is why long sessions were being sliced into
// dozens of small chunks against the default 4096-token context.
type ollama struct {
	c  Config
	hc *http.Client
}

func (p *ollama) Name() string        { return p.c.Name }
func (p *ollama) Model() string       { return p.c.Model }
func (p *ollama) Cloud() bool         { return p.c.Cloud }
func (p *ollama) MaxInputTokens() int { return p.c.MaxInputTokens }

func (p *ollama) Complete(ctx context.Context, req Request) (Response, error) {
	msgs := []map[string]string{}
	if req.System != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": req.User})
	numPredict := req.MaxTokens
	if numPredict == 0 {
		numPredict = -1
	}
	body := map[string]any{
		"model":    p.c.Model,
		"messages": msgs,
		"stream":   false,
		"options": map[string]any{
			"num_ctx":     p.c.NumCtx,
			"temperature": 0.2,
			"num_predict": numPredict,
		},
	}
	if req.JSON {
		body["format"] = "json"
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.c.BaseURL+"/api/chat", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
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
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.Unmarshal(rb, &out); err != nil {
		return Response{}, fmt.Errorf("ollama: bad response: %s", rb)
	}
	return Response{Text: out.Message.Content, InputTokens: out.PromptEvalCount, OutputTokens: out.EvalCount}, nil
}
