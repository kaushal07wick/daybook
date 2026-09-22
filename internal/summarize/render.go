// Package summarize turns stored events into session summaries and digests.
package summarize

import (
	"strings"

	"github.com/kaushal07wick/daybook/internal/provider"
	"github.com/kaushal07wick/daybook/internal/source"
	"github.com/kaushal07wick/daybook/internal/store"
)

// maxToolRunes caps how much of one tool result reaches the model: a
// single Bash/Read dump can otherwise blow the whole chunk budget on
// output the model rarely needs verbatim.
const maxToolRunes = 800

// Render flattens events into the compact transcript the model reads.
func Render(evs []store.Event) string {
	var sb strings.Builder
	for _, e := range evs {
		text := strings.TrimSpace(e.Text)
		switch e.Role {
		case "user":
			sb.WriteString("U: ")
		case "assistant":
			sb.WriteString("A: ")
		default:
			sb.WriteString("T(" + e.ToolName + "): ")
			if e.Role == "tool" {
				text = source.Truncate(text, maxToolRunes)
			}
		}
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}
	return strings.TrimSpace(sb.String())
}

// Chunk splits text at blank lines into pieces of at most maxTokens
// (estimated). A single oversized block becomes its own chunk.
func Chunk(text string, maxTokens int) []string {
	var out []string
	var cur strings.Builder
	tok := 0
	for _, block := range strings.Split(text, "\n\n") {
		n := provider.EstimateTokens(block)
		if cur.Len() > 0 && tok+n > maxTokens {
			out = append(out, cur.String())
			cur.Reset()
			tok = 0
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(block)
		tok += n
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
