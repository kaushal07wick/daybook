package store

import (
	"context"
	"testing"
)

func TestOpenMigratesToV1(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var v int
	if err := s.DB().QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("user_version = %d, want 1", v)
	}
	for _, tbl := range []string{"sources", "sessions", "events", "summaries", "digests", "terms", "term_links", "fts"} {
		var n int
		if err := s.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE name = ?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (err=%v)", tbl, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		s, err := Open(dir + "/d.db")
		if err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
	}
}
