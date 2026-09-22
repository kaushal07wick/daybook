package source

import (
	"bytes"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/kaushal07wick/daybook/internal/store"
)

// Files lists every transcript file a source currently owns.
func Files(s Source) ([]string, error) {
	var out []string
	for _, g := range s.Globs() {
		m, err := filepath.Glob(g)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", g, err)
		}
		out = append(out, m...)
	}
	return out, nil
}

// IngestFile reads whatever was appended to path since the last call,
// parses it and stores the events. Returns the number of new events.
func IngestFile(st *store.Store, s Source, path string) (int, error) {
	off, err := st.Offset(path)
	if err != nil {
		return 0, err
	}
	data, newOff, err := ReadNew(path, off)
	if err != nil || len(data) == 0 {
		return 0, err
	}
	evs, err := s.Parse(path, bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	stEvs := make([]store.Event, 0, len(evs))
	meta := map[string]store.SessionMeta{}
	for _, e := range evs {
		stEvs = append(stEvs, store.Event{SessionID: e.SessionID, TS: e.TS, Role: string(e.Role), ToolName: e.ToolName, Text: e.Text})
		if e.CWD != "" {
			meta[e.SessionID] = store.SessionMeta{Project: e.Project, CWD: e.CWD}
		}
	}
	if err := st.AppendEvents(s.Kind(), path, newOff, stEvs, meta); err != nil {
		return 0, err
	}
	return len(stEvs), nil
}

// IngestAll walks every registered source once. Used by backfill and at
// serve start-up to catch anything written while daybook was down.
func IngestAll(st *store.Store, log *slog.Logger) (files, events int, err error) {
	for _, s := range All() {
		paths, err := Files(s)
		if err != nil {
			return files, events, err
		}
		for _, p := range paths {
			n, err := IngestFile(st, s, p)
			if err != nil {
				log.Warn("ingest", "path", p, "err", err)
				continue
			}
			if n > 0 {
				files++
				events += n
			}
		}
	}
	return files, events, nil
}
