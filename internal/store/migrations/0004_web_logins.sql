-- Browser logins. The cookie holds a random secret; only its SHA-256 is kept, so a copy of the
-- database does not log anyone in.
CREATE TABLE web_logins (
  hash    TEXT PRIMARY KEY,
  created TEXT NOT NULL,
  agent   TEXT NOT NULL DEFAULT ''
);
