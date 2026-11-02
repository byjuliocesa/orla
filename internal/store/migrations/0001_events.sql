CREATE TABLE events (
  id         TEXT    PRIMARY KEY,
  session    TEXT    NOT NULL,
  seq        INTEGER NOT NULL,
  time       TEXT    NOT NULL,
  run        TEXT    NOT NULL DEFAULT '',
  parent_run TEXT    NOT NULL DEFAULT '',
  source     TEXT    NOT NULL,
  type       TEXT    NOT NULL,
  data       TEXT,
  UNIQUE (session, seq)
);
