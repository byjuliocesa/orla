-- Sessions can start in a terminal, from Claude Code's hooks, instead of from Orla.
ALTER TABLE sessions ADD COLUMN terminal TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN native_prompt TEXT NOT NULL DEFAULT '';
CREATE INDEX sessions_by_native ON sessions (agent, native_session);
CREATE INDEX sessions_by_terminal ON sessions (terminal);
CREATE INDEX runs_by_native_prompt ON runs (session, native_prompt);
