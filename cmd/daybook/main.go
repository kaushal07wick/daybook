// Command daybook ingests AI-coding transcripts and digests them.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "github.com/kaushal07wick/daybook/internal/source/claude"
	_ "github.com/kaushal07wick/daybook/internal/source/codex"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	var err error
	switch os.Args[1] {
	case "backfill":
		err = cmdBackfill(os.Args[2:], log)
	case "status":
		err = cmdStatus(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "daybook:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: daybook <backfill|status|version> [flags]`)
}

// dbPath returns the database location, creating its directory.
func dbPath() string {
	home := os.Getenv("DAYBOOK_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, "Library", "Application Support", "daybook")
	}
	_ = os.MkdirAll(home, 0o755)
	return filepath.Join(home, "daybook.db")
}
