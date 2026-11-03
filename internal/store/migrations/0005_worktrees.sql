-- Worktrees Orla made, so a session working in one is reviewed against the branch it came from.
CREATE TABLE worktrees (
  path    TEXT PRIMARY KEY,
  repo    TEXT NOT NULL,
  branch  TEXT NOT NULL,
  base    TEXT NOT NULL,
  created TEXT NOT NULL,
  -- Set on removal, with what it kept: the stash holding uncommitted work, and whether the
  -- branch stayed because its base lacked its commits.
  removed     TEXT,
  stash       TEXT    NOT NULL DEFAULT '',
  branch_kept INTEGER NOT NULL DEFAULT 0
);
