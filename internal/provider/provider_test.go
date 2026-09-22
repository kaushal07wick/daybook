package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatible(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Name: "t", Type: "openai", BaseURL: srv.URL + "/v1", Model: "m", APIKey: "k"})
	res, err := p.Complete(context.Background(), Request{System: "sys", User: "u", JSON: true})
	if err != nil || res.Text != `{"ok":true}` || res.InputTokens != 10 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got["model"] != "m" || got["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("body %v", got)
	}
}

func TestAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("bad headers")
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":5,"output_tokens":1}}`))
	}))
	defer srv.Close()
	p, _ := New(Config{Type: "anthropic", BaseURL: srv.URL, Model: "m", APIKey: "k", Cloud: true})
	res, err := p.Complete(context.Background(), Request{User: "u"})
	if err != nil || res.Text != "hi" || !p.Cloud() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) }))
	defer srv.Close()
	p, _ := New(Config{Type: "openai", BaseURL: srv.URL, Model: "m"})
	if _, err := p.Complete(context.Background(), Request{User: "u"}); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
}
