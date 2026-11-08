-- The owner's preferences that follow them to every browser, such as how the skills are
-- arranged: one JSON value per key.
CREATE TABLE prefs (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
