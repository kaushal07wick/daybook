package claude

import (
	"os"
	"strings"
	"testing"

	"github.com/kaushal07wick/daybook/internal/source"
)

func TestParseFixture(t *testing.T) {
	f, err := os.Open("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	evs, err := (&Source{}).Parse("/x/proj/s-1.jsonl", f)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		role source.Role
		tool string
		text string
	}{
		{source.User, "", "restart the api on box A"},
		{source.Assistant, "Bash", "$ systemctl restart api"},
		{source.Assistant, "", "On it."},
		{source.Tool, "Bash", "ok\n"},
		{source.Assistant, "", "Restarted; 3 workers up at 40% CPU."},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	for i, w := range want {
		e := evs[i]
		if e.Role != w.role || e.ToolName != w.tool || e.Text != w.text || e.SessionID != "s-1" || e.Kind != "claude" {
			t.Errorf("%d: %+v", i, e)
		}
	}
	if evs[0].Project != "proj/api" || evs[0].CWD != "/home/me/proj/api" || evs[0].TS.IsZero() {
		t.Errorf("meta: %+v", evs[0])
	}
}

func TestParseSkipsGarbageLines(t *testing.T) {
	evs, err := (&Source{}).Parse("p", strings.NewReader("not json\n{\"type\":\"user\"}\n"))
	if err != nil || len(evs) != 0 {
		t.Fatalf("evs=%v err=%v", evs, err)
	}
}

// TestToolNameSurvivesSplitParse guards against tool_use and its tool_result
// arriving in separate Parse calls (the real ingest path parses only the
// bytes appended since the last read): the id->name correlation must
// survive across calls on the same *Source and path.
func TestToolNameSurvivesSplitParse(t *testing.T) {
	toolUse := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"systemctl restart api"}}]},"uuid":"a1","timestamp":"2026-09-01T10:00:05.000Z","cwd":"/home/me/proj/api","sessionId":"s-1","isSidechain":false}` + "\n"
	toolResult := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"t1","type":"tool_result","content":"ok\n","is_error":false}]},"uuid":"u2","timestamp":"2026-09-01T10:00:06.000Z","cwd":"/home/me/proj/api","sessionId":"s-1","isSidechain":false}` + "\n"

	src := &Source{}
	const path = "/x/proj/s-1.jsonl"
	if _, err := src.Parse(path, strings.NewReader(toolUse)); err != nil {
		t.Fatal(err)
	}
	evs, err := src.Parse(path, strings.NewReader(toolResult))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ToolName != "Bash" {
		t.Fatalf("%+v", evs)
	}
}
