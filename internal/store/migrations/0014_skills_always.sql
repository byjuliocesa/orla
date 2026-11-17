-- The skills a session loads whole into every message's instructions.
ALTER TABLE sessions ADD COLUMN skills_always TEXT NOT NULL DEFAULT '[]';
