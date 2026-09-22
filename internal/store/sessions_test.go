package store

import (
	"testing"
	"time"
)

func TestAppendEventsUpsertsSessionAndOffset(t *testing.T) {
	s, _ := Open(":memory:")
	defer func() { _ = s.Close() }()
	t0 := time.Unix(1_700_000_000, 0)
	evs := []Event{
		{SessionID: "s1", TS: t0, Role: "user", Text: "hi"},
		{SessionID: "s1", TS: t0.Add(time.Minute), Role: "assistant", Text: "hello"},
	}
	meta := map[string]SessionMeta{"s1": {Project: "work/x", CWD: "/w/x"}}
	if err := s.AppendEvents("claude", "/p/a.jsonl", 123, evs, meta); err != nil {
		t.Fatal(err)
	}
	off, _ := s.Offset("/p/a.jsonl")
	if off != 123 {
		t.Fatalf("offset %d", off)
	}
	sess, err := s.Session("s1")
	if err != nil || sess.NEvents != 2 || !sess.EndedAt.Equal(t0.Add(time.Minute)) || sess.Project != "work/x" {
		t.Fatalf("session %+v err %v", sess, err)
	}
	// second append bumps ended_at and n_events
	if err := s.AppendEvents("claude", "/p/a.jsonl", 200, []Event{{SessionID: "s1", TS: t0.Add(time.Hour), Role: "user", Text: "more"}}, nil); err != nil {
		t.Fatal(err)
	}
	sess, err = s.Session("s1")
	if err != nil || sess.NEvents != 3 || !sess.EndedAt.Equal(t0.Add(time.Hour)) {
		t.Fatalf("after 2nd append %+v err %v", sess, err)
	}
	got, err := s.Events("s1")
	if err != nil || len(got) != 3 || got[2].Text != "more" {
		t.Fatalf("events %+v err %v", got, err)
	}
}

func TestCloseIdle(t *testing.T) {
	s, _ := Open(":memory:")
	defer func() { _ = s.Close() }()
	t0 := time.Unix(1_700_000_000, 0)
	if err := s.AppendEvents("claude", "/p", 1, []Event{{SessionID: "old", TS: t0, Role: "user", Text: "x"}, {SessionID: "new", TS: t0.Add(2 * time.Hour), Role: "user", Text: "y"}}, nil); err != nil {
		t.Fatal(err)
	}
	ids, err := s.CloseIdle(t0.Add(time.Hour))
	if err != nil || len(ids) != 1 || ids[0] != "old" {
		t.Fatalf("ids %v err %v", ids, err)
	}
	ids, err = s.CloseIdle(t0.Add(time.Hour)) // idempotent
	if err != nil || len(ids) != 0 {
		t.Fatalf("second sweep returned %v err %v", ids, err)
	}
}
