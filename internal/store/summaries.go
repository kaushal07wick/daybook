package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"
)

// Summary is one session's LLM summary. JSON == "" means the model failed
// twice and Raw holds what it said.
type Summary struct {
	SessionID, Provider, Model string
	JSON, Raw                  string
	CreatedAt                  time.Time
}

// Digest is a day or week roll-up.
type Digest struct {
	Period, Key, Provider, Model, JSON, MD string
	CreatedAt                              time.Time
}

// Term is one ledger entry.
type Term struct {
	Term, Kind          string
	FirstSeen, LastSeen time.Time
	N                   int
}

// Hit is one full-text search result.
type Hit struct{ Kind, Key, Snippet string }

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// PutSummary stores (or replaces) a session summary and indexes it.
func (s *Store) PutSummary(x Summary) error {
	return s.Write(func(tx *sql.Tx) error {
		ctx := context.Background()
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO summaries(session_id,provider,model,json,raw,created_at) VALUES(?,?,?,?,?,?)`,
			x.SessionID, x.Provider, x.Model, nullIfEmpty(x.JSON), nullIfEmpty(x.Raw), x.CreatedAt.Unix()); err != nil {
			return err
		}
		if x.JSON == "" {
			return nil
		}
		return putFTS(ctx, tx, "session", x.SessionID, x.JSON)
	})
}

func putFTS(ctx context.Context, tx *sql.Tx, kind, key, body string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM fts WHERE kind = ? AND key = ?`, kind, key); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fts(kind,key,body) VALUES(?,?,?)`, kind, key, body)
	return err
}

// Summary loads one summary; ok is false if none exists.
func (s *Store) Summary(sessionID string) (Summary, bool, error) {
	var x Summary
	var js, raw sql.NullString
	var ts int64
	err := s.db.QueryRowContext(context.Background(), `SELECT session_id,provider,model,json,raw,created_at FROM summaries WHERE session_id = ?`, sessionID).
		Scan(&x.SessionID, &x.Provider, &x.Model, &js, &raw, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return x, false, nil
	}
	if err != nil {
		return x, false, err
	}
	x.JSON, x.Raw, x.CreatedAt = js.String, raw.String, time.Unix(ts, 0)
	return x, true, nil
}

// Unsummarized returns closed sessions with no summary row, oldest first.
func (s *Store) Unsummarized(limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT `+sessionCols+` FROM sessions s WHERE closed = 1 AND NOT EXISTS (SELECT 1 FROM summaries m WHERE m.session_id = s.id) ORDER BY ended_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SummariesBetween returns successful summaries for sessions that ended in [from, to).
func (s *Store) SummariesBetween(from, to time.Time) ([]Summary, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT m.session_id,m.provider,m.model,m.json,m.created_at FROM summaries m JOIN sessions s ON s.id = m.session_id
		WHERE m.json IS NOT NULL AND s.ended_at >= ? AND s.ended_at < ? ORDER BY s.started_at`, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Summary
	for rows.Next() {
		var x Summary
		var ts int64
		if err := rows.Scan(&x.SessionID, &x.Provider, &x.Model, &x.JSON, &ts); err != nil {
			return nil, err
		}
		x.CreatedAt = time.Unix(ts, 0)
		out = append(out, x)
	}
	return out, rows.Err()
}

// PutDigest stores (or replaces) a digest and indexes its markdown.
func (s *Store) PutDigest(d Digest) error {
	return s.Write(func(tx *sql.Tx) error {
		ctx := context.Background()
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO digests(period,key,provider,model,json,md,created_at) VALUES(?,?,?,?,?,?,?)`,
			d.Period, d.Key, d.Provider, d.Model, d.JSON, d.MD, d.CreatedAt.Unix()); err != nil {
			return err
		}
		return putFTS(ctx, tx, d.Period, d.Key, d.MD)
	})
}

// Digest loads one digest.
func (s *Store) Digest(period, key string) (Digest, bool, error) {
	var d Digest
	var ts int64
	err := s.db.QueryRowContext(context.Background(), `SELECT period,key,provider,model,json,md,created_at FROM digests WHERE period = ? AND key = ?`, period, key).
		Scan(&d.Period, &d.Key, &d.Provider, &d.Model, &d.JSON, &d.MD, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	if err != nil {
		return d, false, err
	}
	d.CreatedAt = time.Unix(ts, 0)
	return d, true, nil
}

// DigestKeys lists existing digest keys for a period, newest first.
func (s *Store) DigestKeys(period string) ([]string, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT key FROM digests WHERE period = ? ORDER BY key DESC`, period)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// UpsertTerms records ledger terms seen in a session. terms maps kind → values.
func (s *Store) UpsertTerms(sessionID string, at time.Time, terms map[string][]string) error {
	return s.Write(func(tx *sql.Tx) error {
		ctx := context.Background()
		for kind, vals := range terms {
			for _, v := range vals {
				if v == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO terms(term,kind,first_seen,last_seen,n) VALUES(?,?,?,?,1)
					ON CONFLICT(term,kind) DO UPDATE SET last_seen = max(last_seen, excluded.last_seen), first_seen = min(first_seen, excluded.first_seen), n = n + 1`,
					v, kind, at.Unix(), at.Unix()); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO term_links(term,kind,session_id) VALUES(?,?,?)`, v, kind, sessionID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Terms lists the ledger, most frequent first. kind "" means all kinds.
func (s *Store) Terms(kind string, limit int) ([]Term, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT term,kind,first_seen,last_seen,n FROM terms WHERE (? = '' OR kind = ?) ORDER BY n DESC, last_seen DESC LIMIT ?`, kind, kind, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Term
	for rows.Next() {
		var t Term
		var f, l int64
		if err := rows.Scan(&t.Term, &t.Kind, &f, &l, &t.N); err != nil {
			return nil, err
		}
		t.FirstSeen, t.LastSeen = time.Unix(f, 0), time.Unix(l, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TermSessions lists the sessions a term appeared in.
func (s *Store) TermSessions(term, kind string) ([]string, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT session_id FROM term_links WHERE term = ? AND kind = ?`, term, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Search runs an FTS5 query over summaries and digests. A query FTS5 can't
// parse (unbalanced quotes, dangling operators) is treated as "no hits"
// rather than an error — this feeds a loopback search box.
func (s *Store) Search(q string, limit int) ([]Hit, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT kind, key, snippet(fts, 2, '<b>', '</b>', '…', 12) FROM fts WHERE fts MATCH ? ORDER BY rank LIMIT ?`, q, limit)
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Kind, &h.Key, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
