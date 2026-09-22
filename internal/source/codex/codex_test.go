package codex

import (
	"os"
	"strings"
	"testing"

	"github.com/kaushal07wick/daybook/internal/source"
)

func TestParseFixture(t *testing.T) {
	f, err := os.Open("testdata/rollout.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	evs, err := (&Source{}).Parse("/x/rollout-2026-04-04T11-47-38-c-1.jsonl", f)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		role source.Role
		tool string
		text string
	}{
		{source.User, "", "show me the network config"},
		{source.Assistant, "", "Collecting it now."},
		{source.Assistant, "exec_command", "$ ip -brief addr"},
		{source.Tool, "exec_command", "eth0 UP 10.0.0.5/24"},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d: %+v", len(evs), evs)
	}
	for i, w := range want {
		e := evs[i]
		if e.Role != w.role || e.ToolName != w.tool || e.Text != w.text || e.SessionID != "c-1" || e.Kind != "codex" || e.Project != "proj/web" {
			t.Errorf("%d: %+v", i, e)
		}
	}
}

func TestSessionIDFallsBackToFilename(t *testing.T) {
	evs, err := (&Source{}).Parse("/x/rollout-2026-04-04T11-47-38-abc.jsonl", strings.NewReader(`{"timestamp":"2026-04-04T11:49:03.140Z","type":"event_msg","payload":{"type":"user_message","message":"hi"}}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].SessionID != "abc" {
		t.Fatalf("%+v", evs)
	}
}

// TestToolNameSurvivesSplitParse guards against function_call and its
// function_call_output arriving in separate Parse calls (the real ingest
// path parses only the bytes appended since the last read): the
// call_id->name correlation must survive across calls on the same *Source
// and path.
func TestToolNameSurvivesSplitParse(t *testing.T) {
	call := `{"timestamp":"2026-04-04T11:49:19.435Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"ip -brief addr\"}"}}` + "\n"
	output := `{"timestamp":"2026-04-04T11:49:19.537Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"eth0 UP 10.0.0.5/24"}}` + "\n"

	src := &Source{}
	const path = "/x/rollout-2026-04-04T11-47-38-c-1.jsonl"
	if _, err := src.Parse(path, strings.NewReader(call)); err != nil {
		t.Fatal(err)
	}
	evs, err := src.Parse(path, strings.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].ToolName != "exec_command" {
		t.Fatalf("%+v", evs)
	}
}
