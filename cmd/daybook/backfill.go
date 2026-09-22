package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kaushal07wick/daybook/internal/config"
	"github.com/kaushal07wick/daybook/internal/provider"
	"github.com/kaushal07wick/daybook/internal/source"
	"github.com/kaushal07wick/daybook/internal/store"
	"github.com/kaushal07wick/daybook/internal/summarize"
)

func cmdBackfill(args []string, log *slog.Logger) error {
	fs := flag.NewFlagSet("backfill", flag.ExitOnError)
	noLLM := fs.Bool("no-llm", false, "ingest only, do not summarise")
	limit := fs.Int("limit", 1<<30, "max sessions to summarise")
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
	cfg, err := loadConfig(log)
	if err != nil {
		return err
	}
	pc, err := cfg.Provider(cfg.DefaultProvider)
	if err != nil {
		return err
	}
	prov, err := provider.New(pc)
	if err != nil {
		return err
	}
	if _, err := st.CloseIdle(time.Now().Add(-cfg.CloseAfter)); err != nil {
		return err
	}
	z := &summarize.Summarizer{Store: st, Provider: prov, Redact: cfg.Privacy.Redact, Log: log}
	done, failed, err := z.Drain(context.Background(), *limit, pc.Concurrency)
	log.Info("summarised", "done", done, "failed", failed, "provider", pc.Name, "model", pc.Model)
	return err
}

// loadConfig reads the config file, writing the default one on first run so
// the user has something to edit.
func loadConfig(log *slog.Logger) (config.Config, error) {
	path := config.Path()
	_, statErr := os.Stat(path)
	cfg, err := config.Load(path)
	if err != nil {
		return cfg, err
	}
	if errors.Is(statErr, os.ErrNotExist) {
		if err := cfg.Write(path); err != nil {
			return cfg, err
		}
		log.Info("wrote default config", "path", path)
	}
	return cfg, nil
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
