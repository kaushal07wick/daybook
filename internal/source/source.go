// Package source defines the transcript sources daybook ingests and the
// normalised Event they all produce.
package source

import (
	"io"
	"path/filepath"
	"strings"
	"time"
)

// MaxToolText caps stored tool output, in runes, so one `cat` of a log can't
// bloat the DB.
const MaxToolText = 2048

// Role is who produced an event.
type Role string

// Roles an event can carry.
const (
	User      Role = "user"
	Assistant Role = "assistant"
	Tool      Role = "tool"
)

// Event is one normalised transcript line.
type Event struct {
	SessionID string
	Kind      string
	Project   string
	CWD       string
	TS        time.Time
	Role      Role
	ToolName  string
	Text      string
}

// Source is one agent's transcript format.
type Source interface {
	Kind() string
	Globs() []string
	Parse(path string, r io.Reader) ([]Event, error)
}

var registry []Source

// Register adds a Source; parsers call it from init().
func Register(s Source) { registry = append(registry, s) }

// All returns registered sources in registration order.
func All() []Source { return registry }

// ProjectOf derives a short project label from a working directory:
// the last two path elements ("work/fleet-manager").
func ProjectOf(cwd string) string {
	cwd = filepath.Clean(cwd)
	if cwd == "" || cwd == "." || cwd == "/" {
		return ""
	}
	parts := strings.Split(strings.Trim(cwd, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return parts[len(parts)-1]
}

// Truncate cuts s to n runes, marking the cut.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…[truncated]"
}
