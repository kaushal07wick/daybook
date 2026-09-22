// Package codex parses OpenAI Codex CLI rollouts (~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl).
package codex

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kaushal07wick/daybook/internal/source"
)

// Source is the Codex CLI transcript source.
type Source struct{}

func init() { source.Register(Source{}) }

// Kind implements source.Source.
func (Source) Kind() string { return "codex" }

// Globs implements source.Source.
func (Source) Globs() []string {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".codex")
	}
	return []string{filepath.Join(dir, "sessions", "*", "*", "*", "*.jsonl")}
}

type line struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   struct {
		ID        string `json:"id"`
		CWD       string `json:"cwd"`
		Type      string `json:"type"`
		Role      string `json:"role"`
		Message   string `json:"message"`
		Name      string `json:"name"`
		CallID    string `json:"call_id"`
		Arguments string `json:"arguments"`
		Output    string `json:"output"`
	} `json:"payload"`
}

var filenameID = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-(.+)\.jsonl$`)

// Parse implements source.Source.
func (s Source) Parse(path string, r io.Reader) ([]source.Event, error) {
	id := ""
	if m := filenameID.FindStringSubmatch(filepath.Base(path)); m != nil {
		id = m[1]
	}
	cwd := ""
	tools := map[string]string{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var out []source.Event
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue
		}
		if l.Type == "session_meta" {
			if l.Payload.ID != "" {
				id = l.Payload.ID
			}
			cwd = l.Payload.CWD
			continue
		}
		if id == "" {
			continue
		}
		e := source.Event{SessionID: id, Kind: s.Kind(), Project: source.ProjectOf(cwd), CWD: cwd, TS: l.Timestamp}
		p := l.Payload
		switch {
		case l.Type == "event_msg" && p.Type == "user_message":
			e.Role, e.Text = source.User, p.Message
		case l.Type == "event_msg" && p.Type == "agent_message":
			e.Role, e.Text = source.Assistant, p.Message
		case l.Type == "response_item" && p.Type == "function_call":
			tools[p.CallID] = p.Name
			e.Role, e.ToolName, e.Text = source.Assistant, p.Name, "$ "+cmdOf(p.Arguments)
		case l.Type == "response_item" && p.Type == "function_call_output":
			e.Role, e.ToolName = source.Tool, tools[p.CallID]
			e.Text = source.Truncate(p.Output, source.MaxToolText)
		default:
			continue
		}
		if strings.TrimSpace(e.Text) == "" {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// cmdOf pulls the shell command out of a function_call's JSON arguments.
func cmdOf(args string) string {
	var a struct {
		Cmd string `json:"cmd"`
	}
	if json.Unmarshal([]byte(args), &a) == nil && a.Cmd != "" {
		return a.Cmd
	}
	return source.Truncate(args, 300)
}
