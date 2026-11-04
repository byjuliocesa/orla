-- What the owner sets on a session from the sidebar: a name of their own, a pin that keeps it on
-- top, and when it was archived out of the list.
ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN archived TEXT;
