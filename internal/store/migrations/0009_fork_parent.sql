-- The Orla session and run a fork came from, so it can show the conversation it goes on from.
ALTER TABLE sessions ADD COLUMN fork_parent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN fork_run TEXT NOT NULL DEFAULT '';
