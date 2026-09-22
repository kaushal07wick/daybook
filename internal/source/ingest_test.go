package source_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kaushal07wick/daybook/internal/source"
	"github.com/kaushal07wick/daybook/internal/store"
)

type fake struct{ glob string }

func (f fake) Kind() string    { return "fake" }
func (f fake) Globs() []string { return []string{f.glob} }
func (fake) Parse(_ string, r io.Reader) ([]source.Event, error) {
	b, _ := io.ReadAll(r)
	var out []source.Event
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		out = append(out, source.Event{SessionID: "s", Kind: "fake", TS: time.Unix(1, 0), Role: source.User, Text: l})
	}
	return out, nil
}

func TestIngestFileIsIncremental(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Open(":memory:")
	defer func() { _ = st.Close() }()
	src := fake{glob: filepath.Join(dir, "*.jsonl")}
	if n, err := source.IngestFile(st, src, p); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := source.IngestFile(st, src, p); n != 0 {
		t.Fatalf("re-ingest n=%d", n)
	}
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _ := source.IngestFile(st, src, p); n != 1 {
		t.Fatalf("append n=%d", n)
	}
	_, ev, _, _ := st.Counts()
	if ev != 3 {
		t.Fatalf("events=%d", ev)
	}
}
