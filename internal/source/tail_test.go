package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadNewReturnsWholeLinesOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(p, []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, off, err := ReadNew(p, 0)
	if err != nil || string(data) != "{\"a\":1}\n{\"b\":2}\n" || off != 16 {
		t.Fatalf("data=%q off=%d err=%v", data, off, err)
	}
	if err := os.WriteFile(p, []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, off, _ = ReadNew(p, off)
	if string(data) != "{\"c\":3}\n" || off != 24 {
		t.Fatalf("second read data=%q off=%d", data, off)
	}
}

func TestReadNewHandlesTruncatedFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, off, err := ReadNew(p, 100) // offset beyond EOF → restart from 0
	if err != nil || off != 2 {
		t.Fatalf("off=%d err=%v", off, err)
	}
}

func TestProjectOf(t *testing.T) {
	if got := ProjectOf("/Users/k/Desktop/workspace/work/fleet-manager"); got != "work/fleet-manager" {
		t.Fatal(got)
	}
	if got := ProjectOf("/tmp"); got != "tmp" {
		t.Fatal(got)
	}
	if got := ProjectOf(""); got != "" {
		t.Fatal(got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("héllo", 3); got != "hél…[truncated]" {
		t.Fatalf("%q", got)
	}
	if got := Truncate("ok", 10); got != "ok" {
		t.Fatalf("%q", got)
	}
}
