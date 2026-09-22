// Package config loads ~/.config/daybook/config.toml and supplies defaults.
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/kaushal07wick/daybook/internal/provider"
)

// ProviderBlock is one [providers.<name>] table.
type ProviderBlock struct {
	Type           string `toml:"type"`
	BaseURL        string `toml:"base_url"`
	Model          string `toml:"model"`
	APIKeyEnv      string `toml:"api_key_env"`
	Cloud          bool   `toml:"cloud"`
	MaxInputTokens int    `toml:"max_input_tokens"`
	Concurrency    int    `toml:"concurrency"`
}

// Config is the whole file.
type Config struct {
	DefaultProvider string                   `toml:"default_provider"`
	Providers       map[string]ProviderBlock `toml:"providers"`
	Privacy         struct {
		Redact []string `toml:"redact"`
	} `toml:"privacy"`
	CloseAfter time.Duration `toml:"close_after"`
	Listen     string        `toml:"listen"`
	DigestAt   string        `toml:"digest_at"`
}

const ollamaURL = "http://127.0.0.1:11434"

// Path is where the config lives.
func Path() string {
	if p := os.Getenv("DAYBOOK_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "daybook", "config.toml")
}

// Default is the zero-config setup: local Ollama, no cloud.
func Default() Config {
	c := Config{DefaultProvider: "local", Providers: map[string]ProviderBlock{
		// Ollama serves models at a 4096-token context unless
		// OLLAMA_CONTEXT_LENGTH says otherwise, and its OpenAI endpoint
		// silently truncates longer prompts, so budget well under that.
		"local": {Type: "openai", BaseURL: ollamaURL + "/v1", Model: "qwen2.5:3b", MaxInputTokens: 3000},
	}}
	c.fill()
	return c
}

func (c *Config) fill() {
	if c.CloseAfter == 0 {
		c.CloseAfter = 30 * time.Minute
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:7331"
	}
	if c.DigestAt == "" {
		c.DigestAt = "23:55"
	}
}

// Load reads path. A missing file yields Default() (with the Ollama model
// probed if reachable); a malformed file is an error.
func Load(path string) (Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, os.ErrNotExist) {
		c = Default()
		if m, ok := ProbeOllama(ollamaURL); ok {
			p := c.Providers["local"]
			p.Model = m
			c.Providers["local"] = p
		}
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return c, fmt.Errorf("config %s: unknown key %s", path, u[0])
	}
	c.fill()
	if _, ok := c.Providers[c.DefaultProvider]; !ok {
		return c, fmt.Errorf("config %s: default_provider %q not defined", path, c.DefaultProvider)
	}
	return c, nil
}

// ProbeOllama asks a running Ollama which models it has and picks one.
func ProbeOllama(baseURL string) (string, bool) {
	hc := http.Client{Timeout: 2 * time.Second}
	hr, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return "", false
	}
	resp, err := hc.Do(hr)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Models []struct{ Name string } `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || len(out.Models) == 0 {
		return "", false
	}
	for _, m := range out.Models {
		if m.Name == "qwen2.5:3b" {
			return m.Name, true
		}
	}
	return out.Models[0].Name, true
}

// Provider resolves a named block into a provider.Config with the API key
// read from the environment.
func (c Config) Provider(name string) (provider.Config, error) {
	b, ok := c.Providers[name]
	if !ok {
		return provider.Config{}, fmt.Errorf("provider %q not in config", name)
	}
	pc := provider.Config{Name: name, Type: b.Type, BaseURL: b.BaseURL, Model: b.Model, Cloud: b.Cloud, MaxInputTokens: b.MaxInputTokens, Concurrency: b.Concurrency}
	if b.APIKeyEnv != "" {
		pc.APIKey = os.Getenv(b.APIKeyEnv)
		if pc.APIKey == "" {
			return pc, fmt.Errorf("provider %q: $%s is empty", name, b.APIKeyEnv)
		}
	}
	if pc.Concurrency == 0 {
		if b.Cloud {
			pc.Concurrency = 4
		} else {
			pc.Concurrency = 1
		}
	}
	return pc, nil
}

// Write persists c (used once, on first run, so the user has a file to edit).
func (c Config) Write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(c)
}
