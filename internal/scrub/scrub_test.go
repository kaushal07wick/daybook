package scrub

import (
	"os"
	"testing"
)

func TestGolden(t *testing.T) {
	in, _ := os.ReadFile("testdata/in.txt")
	want, _ := os.ReadFile("testdata/out.txt")
	got := Text(string(in), []string{"Acme Corp"})
	if got != string(want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if again := Text(got, []string{"Acme Corp"}); again != got {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

func TestPlaceholdersAreStable(t *testing.T) {
	got := Text("a 1.2.3.4 b 5.6.7.8 c 1.2.3.4", nil)
	if got != "a <ip-1> b <ip-2> c <ip-1>" {
		t.Fatal(got)
	}
}
