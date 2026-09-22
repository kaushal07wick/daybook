// Package claude parses Claude Code transcripts (~/.claude/projects/*/*.jsonl).
package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaushal07wick/daybook/internal/source"
)

// Source is the Claude Code transcript source.
type Source struct{}

func init() { source.Register(Source{}) }

// Kind implements source.Source.
func (Source) Kind() string { return "claude" }

// Globs implements source.Source.
func (Source) Globs() []string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return []string{filepath.Join(dir, "projects", "*", "*.jsonl")}
}

type line struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	Timestamp   time.Time       `json:"timestamp"`
	CWD         string          `json:"cwd"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// Parse implements source.Source. Unknown or malformed lines are skipped,
// never fatal: a transcript is append-only and one bad line must not block
// the rest.
func (s Source) Parse(_ string, r io.Reader) ([]source.Event, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	toolNames := map[string]string{}
	var out []source.Event
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil || l.IsSidechain || l.SessionID == "" {
			continue
		}
		if l.Type != "user" && l.Type != "assistant" {
			continue
		}
		var m message
		if err := json.Unmarshal(l.Message, &m); err != nil {
			continue
		}
		base := source.Event{SessionID: l.SessionID, Kind: s.Kind(), Project: source.ProjectOf(l.CWD), CWD: l.CWD, TS: l.Timestamp}

		// content is either a bare string or a list of blocks
		var str string
		if json.Unmarshal(m.Content, &str) == nil {
			if l.Type == "user" && keep(str) {
				e := base
				e.Role, e.Text = source.User, str
				out = append(out, e)
			}
			continue
		}
		var blocks []block
		if json.Unmarshal(m.Content, &blocks) != nil {
			continue
		}
		var texts []string
		for _, b := range blocks {
			switch b.Type {
			case "text":
				texts = append(texts, b.Text)
			case "tool_use":
				toolNames[b.ID] = b.Name
				e := base
				e.Role, e.ToolName, e.Text = source.Assistant, b.Name, "$ "+toolInput(b.Input)
				out = append(out, e)
			case "tool_result":
				e := base
				e.Role, e.ToolName = source.Tool, toolNames[b.ToolUseID]
				e.Text = source.Truncate(resultText(b.Content), source.MaxToolText)
				out = append(out, e)
			}
		}
		if t := strings.TrimSpace(strings.Join(texts, "\n")); t != "" && keep(t) {
			e := base
			e.Text = t
			if l.Type == "user" {
				e.Role = source.User
			} else {
				e.Role = source.Assistant
			}
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// keep drops harness noise that carries no work signal.
func keep(t string) bool {
	return !strings.HasPrefix(t, "[Request interrupted") && !strings.HasPrefix(t, "<system-reminder>")
}

// toolInput renders a tool_use input as one line: the command for Bash-like
// tools, the description if present, else compact JSON.
func toolInput(raw json.RawMessage) string {
	var in struct {
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(raw, &in)
	switch {
	case in.Command != "":
		return in.Command
	case in.Description != "":
		return in.Description
	}
	return source.Truncate(string(raw), 300)
}

// resultText flattens a tool_result content (string or text blocks).
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
