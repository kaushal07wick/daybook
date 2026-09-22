CREATE TABLE sources (
  id          INTEGER PRIMARY KEY,
  kind        TEXT NOT NULL,
  path        TEXT NOT NULL UNIQUE,
  byte_offset INTEGER NOT NULL DEFAULT 0,
  mtime       INTEGER NOT NULL DEFAULT 0,
  last_seen   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  kind       TEXT NOT NULL,
  project    TEXT NOT NULL DEFAULT '',
  cwd        TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  ended_at   INTEGER NOT NULL,
  closed     INTEGER NOT NULL DEFAULT 0,
  n_events   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX sessions_ended ON sessions(ended_at);
CREATE TABLE events (
  id         INTEGER PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  ts         INTEGER NOT NULL,
  role       TEXT NOT NULL,
  tool_name  TEXT NOT NULL DEFAULT '',
  text       TEXT NOT NULL
);
CREATE INDEX events_session ON events(session_id, id);
CREATE TABLE summaries (
  session_id TEXT PRIMARY KEY REFERENCES sessions(id),
  provider   TEXT NOT NULL,
  model      TEXT NOT NULL,
  json       TEXT,
  raw        TEXT,
  created_at INTEGER NOT NULL
);
CREATE TABLE digests (
  period     TEXT NOT NULL,
  key        TEXT NOT NULL,
  provider   TEXT NOT NULL,
  model      TEXT NOT NULL,
  json       TEXT NOT NULL,
  md         TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (period, key)
);
CREATE TABLE terms (
  term       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  first_seen INTEGER NOT NULL,
  last_seen  INTEGER NOT NULL,
  n          INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (term, kind)
);
CREATE TABLE term_links (
  term       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  session_id TEXT NOT NULL,
  PRIMARY KEY (term, kind, session_id)
);
CREATE VIRTUAL TABLE fts USING fts5(kind, key, body);
