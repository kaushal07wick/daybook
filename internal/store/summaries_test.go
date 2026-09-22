package store

import (
	"testing"
	"time"
)

func seed(t *testing.T) (*Store, time.Time) {
	s, _ := Open(":memory:")
	t.Cleanup(func() { _ = s.Close() })
	t0 := time.Unix(1_700_000_000, 0)
	_ = s.AppendEvents("claude", "/p", 1, []Event{{SessionID: "a", TS: t0, Role: "user", Text: "x"}, {SessionID: "b", TS: t0.Add(time.Hour), Role: "user", Text: "y"}}, nil)
	_, _ = s.CloseIdle(t0.Add(2 * time.Hour))
	return s, t0
}

func TestUnsummarizedThenPut(t *testing.T) {
	s, t0 := seed(t)
	ids, _ := s.Unsummarized(10)
	if len(ids) != 2 || ids[0].ID != "a" {
		t.Fatalf("%+v", ids)
	}
	_ = s.PutSummary(Summary{SessionID: "a", Provider: "p", Model: "m", JSON: `{"headline":"fixed nvenc on box"}`, CreatedAt: t0})
	ids, _ = s.Unsummarized(10)
	if len(ids) != 1 || ids[0].ID != "b" {
		t.Fatalf("%+v", ids)
	}
	got, ok, _ := s.Summary("a")
	if !ok || got.JSON == "" {
		t.Fatal("missing")
	}
	hits, _ := s.Search("nvenc", 5)
	if len(hits) != 1 || hits[0].Kind != "session" || hits[0].Key != "a" {
		t.Fatalf("%+v", hits)
	}
	sums, _ := s.SummariesBetween(t0.Add(-time.Minute), t0.Add(time.Minute))
	if len(sums) != 1 {
		t.Fatalf("%+v", sums)
	}
}

func TestTerms(t *testing.T) {
	s, t0 := seed(t)
	_ = s.UpsertTerms("a", t0, map[string][]string{"tech": {"TensorRT", "Go"}, "host": {"box-1"}})
	_ = s.UpsertTerms("b", t0.Add(time.Hour), map[string][]string{"tech": {"TensorRT"}})
	terms, _ := s.Terms("tech", 10)
	if len(terms) != 2 || terms[0].Term != "TensorRT" || terms[0].N != 2 || !terms[0].LastSeen.Equal(t0.Add(time.Hour)) {
		t.Fatalf("%+v", terms)
	}
	links, _ := s.TermSessions("TensorRT", "tech")
	if len(links) != 2 {
		t.Fatalf("%v", links)
	}
}

func TestDigests(t *testing.T) {
	s, t0 := seed(t)
	_ = s.PutDigest(Digest{Period: "day", Key: "2026-09-21", Provider: "p", Model: "m", JSON: "{}", MD: "# hello nvdec", CreatedAt: t0})
	_ = s.PutDigest(Digest{Period: "day", Key: "2026-09-22", Provider: "p", Model: "m", JSON: "{}", MD: "# two", CreatedAt: t0})
	keys, _ := s.DigestKeys("day")
	if len(keys) != 2 || keys[0] != "2026-09-22" {
		t.Fatalf("%v", keys)
	}
	d, ok, _ := s.Digest("day", "2026-09-21")
	if !ok || d.MD != "# hello nvdec" {
		t.Fatalf("%+v", d)
	}
	hits, _ := s.Search("nvdec", 5)
	if len(hits) != 1 || hits[0].Kind != "day" {
		t.Fatalf("%+v", hits)
	}
}

func TestSearchBadQuery(t *testing.T) {
	s, _ := seed(t)
	hits, err := s.Search(`"`, 5)
	if err != nil || hits != nil {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
}
