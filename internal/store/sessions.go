package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Session is one agent conversation (one transcript file).
type Session struct {
	ID, Kind, Project, CWD string
	StartedAt, EndedAt     time.Time
	Closed                 bool
	NEvents                int
}

// Event is one message-ish line inside a session.
type Event struct {
	ID        int64
	SessionID string
	TS        time.Time
	Role      string // user | assistant | tool
	ToolName  string
	Text      string
}

// SessionMeta carries the per-session fields a parser learns from the file.
type SessionMeta struct{ Project, CWD string }

// Offset returns the byte offset already ingested for path (0 if unseen).
func (s *Store) Offset(path string) (int64, error) {
	var off int64
	err := s.db.QueryRowContext(context.Background(), `SELECT byte_offset FROM sources WHERE path = ?`, path).Scan(&off)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return off, err
}

// AppendEvents stores evs, upserts their sessions, and records newOffset for
// path — all in one transaction so a crash never leaves them out of step.
func (s *Store) AppendEvents(kind, path string, newOffset int64, evs []Event, meta map[string]SessionMeta) error {
	return s.Write(func(tx *sql.Tx) error {
		ctx := context.Background()
		now := time.Now().Unix()
		if _, err := tx.ExecContext(ctx, `INSERT INTO sources(kind,path,byte_offset,last_seen) VALUES(?,?,?,?)
			ON CONFLICT(path) DO UPDATE SET byte_offset=excluded.byte_offset, last_seen=excluded.last_seen`,
			kind, path, newOffset, now); err != nil {
			return err
		}
		for _, e := range evs {
			m := meta[e.SessionID]
			ts := e.TS.Unix()
			if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id,kind,project,cwd,started_at,ended_at,n_events) VALUES(?,?,?,?,?,?,1)
				ON CONFLICT(id) DO UPDATE SET
				  ended_at = max(ended_at, excluded.ended_at),
				  started_at = min(started_at, excluded.started_at),
				  n_events = n_events + 1,
				  project = CASE WHEN excluded.project != '' THEN excluded.project ELSE project END,
				  cwd     = CASE WHEN excluded.cwd     != '' THEN excluded.cwd     ELSE cwd     END`,
				e.SessionID, kind, m.Project, m.CWD, ts, ts); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO events(session_id,ts,role,tool_name,text) VALUES(?,?,?,?,?)`,
				e.SessionID, ts, e.Role, e.ToolName, e.Text); err != nil {
				return err
			}
		}
		return nil
	})
}

const sessionCols = `id,kind,project,cwd,started_at,ended_at,closed,n_events`

func scanSession(r interface{ Scan(...any) error }) (Session, error) {
	var x Session
	var st, en int64
	var closed int
	err := r.Scan(&x.ID, &x.Kind, &x.Project, &x.CWD, &st, &en, &closed, &x.NEvents)
	x.StartedAt, x.EndedAt, x.Closed = time.Unix(st, 0), time.Unix(en, 0), closed == 1
	return x, err
}

// Session loads one session by id.
func (s *Store) Session(id string) (Session, error) {
	return scanSession(s.db.QueryRowContext(context.Background(), `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
}

// SessionsBetween returns sessions whose last activity falls in [from, to).
func (s *Store) SessionsBetween(from, to time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT `+sessionCols+` FROM sessions WHERE ended_at >= ? AND ended_at < ? ORDER BY started_at`, from.Unix(), to.Unix())
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

// Events returns a session's events in file order.
func (s *Store) Events(sessionID string) ([]Event, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT id,session_id,ts,role,tool_name,text FROM events WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var e Event
		var ts int64
		if err := rows.Scan(&e.ID, &e.SessionID, &ts, &e.Role, &e.ToolName, &e.Text); err != nil {
			return nil, err
		}
		e.TS = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CloseIdle marks every open session idle since before as closed and
// returns their ids so the caller can enqueue them.
func (s *Store) CloseIdle(before time.Time) ([]string, error) {
	var ids []string
	err := s.Write(func(tx *sql.Tx) error {
		ctx := context.Background()
		rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE closed = 0 AND ended_at < ?`, before.Unix())
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET closed = 1 WHERE closed = 0 AND ended_at < ?`, before.Unix())
		return err
	})
	return ids, err
}

// Counts is what `daybook status` prints.
func (s *Store) Counts() (sessions, events, summaries int, err error) {
	err = s.db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM sessions),(SELECT count(*) FROM events),(SELECT count(*) FROM summaries WHERE json IS NOT NULL)`).Scan(&sessions, &events, &summaries)
	return
}
