package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesProvidersAndEnvKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	_ = os.WriteFile(p, []byte(`
default_provider = "cl"
[providers.cl]
type = "anthropic"
model = "m"
api_key_env = "TEST_KEY"
cloud = true
[privacy]
redact = ["Acme"]
`), 0o644)
	t.Setenv("TEST_KEY", "sekrit")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := c.Provider("cl")
	if err != nil || pc.APIKey != "sekrit" || !pc.Cloud || pc.Concurrency != 4 || c.CloseAfter.Minutes() != 30 {
		t.Fatalf("%+v err=%v", pc, err)
	}
	if _, err := c.Provider("nope"); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestProbeOllamaPrefersQwen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:3b"},{"name":"qwen2.5:3b"}]}`))
	}))
	defer srv.Close()
	m, ok := ProbeOllama(srv.URL)
	if !ok || m != "qwen2.5:3b" {
		t.Fatalf("%q %v", m, ok)
	}
}

func TestLoadMissingFileFallsBackToDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || c.DefaultProvider != "local" || c.Listen != "127.0.0.1:7331" {
		t.Fatalf("%+v err=%v", c, err)
	}
}

func TestExampleDecodes(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "x")
	c, err := Load("example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Provider("claude"); err != nil {
		t.Fatal(err)
	}
}
