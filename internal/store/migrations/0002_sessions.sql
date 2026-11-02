CREATE TABLE sessions (
  id             TEXT PRIMARY KEY,
  agent          TEXT NOT NULL,
  cwd            TEXT NOT NULL,
  native_session TEXT NOT NULL DEFAULT '',
  created        TEXT NOT NULL
);

CREATE TABLE runs (
  id         TEXT PRIMARY KEY,
  session    TEXT NOT NULL REFERENCES sessions (id),
  parent_run TEXT NOT NULL DEFAULT '',
  prompt     TEXT NOT NULL,
  status     TEXT NOT NULL,
  started    TEXT NOT NULL,
  finished   TEXT
);

CREATE INDEX runs_by_session ON runs (session, started);
CREATE INDEX runs_by_status ON runs (status);
