-- Where in the forked conversation a fork goes on from: the CLI's id for the last message kept.
-- Empty goes on from the end.
ALTER TABLE sessions ADD COLUMN fork_at TEXT NOT NULL DEFAULT '';
