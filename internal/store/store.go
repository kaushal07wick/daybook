// Package store owns the SQLite schema and every query daybook runs.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"sync"

	_ "modernc.org/sqlite" // register the "sqlite" driver used by sql.Open
)

//go:embed schema.sql
var schemaV1 string

// Store wraps one SQLite database. All writes go through Write so SQLite
// only ever sees a single writer; reads may run concurrently.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// Open opens (or creates) the database at path and migrates it.
// ":memory:" is accepted for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&v); err != nil {
		return fmt.Errorf("user_version: %w", err)
	}
	if v >= 1 {
		return nil
	}
	return s.Write(func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), schemaV1); err != nil {
			return fmt.Errorf("schema v1: %w", err)
		}
		_, err := tx.ExecContext(context.Background(), "PRAGMA user_version = 1")
		return err
	})
}

// Write runs fn inside a transaction, serialised with every other Write.
func (s *Store) Write(fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DB exposes the handle for read queries.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }
