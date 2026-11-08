-- The flows a session follows, as a JSON list of skill names. Every other flow is left out of
-- its conversations, so a session never follows another's way of working.
ALTER TABLE sessions ADD COLUMN flows TEXT NOT NULL DEFAULT '[]';
