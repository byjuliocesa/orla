-- The skills a session leaves out of its conversation, as a JSON list of names. Every skill is
-- the owner's, for all sessions; each session can turn some off.
ALTER TABLE sessions ADD COLUMN skills_off TEXT NOT NULL DEFAULT '[]';
