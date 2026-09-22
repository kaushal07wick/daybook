package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/kaushal07wick/daybook/internal/source"
	"github.com/kaushal07wick/daybook/internal/store"
)

func cmdBackfill(args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("backfill", flag.ExitOnError)
	noLLM := fs.Bool("no-llm", false, "ingest only, do not summarise")
	_ = fs.Parse(args)
	st, err := store.Open(dbPath())
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	files, events, err := source.IngestAll(st, log)
	if err != nil {
		return err
	}
	log.Info("ingested", "files", files, "events", events)
	if *noLLM {
		return nil
	}
	return errors.New("summarisation not implemented yet") // replaced in Task 11
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	_ = fs.Parse(args)
	st, err := store.Open(dbPath())
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	sessions, events, summaries, err := st.Counts()
	if err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"sessions": sessions, "events": events, "summaries": summaries, "version": version})
	}
	fmt.Printf("sessions %d  events %d  summaries %d\n", sessions, events, summaries)
	return nil
}
