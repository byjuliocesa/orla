-- A session forked from another starts by resuming that one's CLI conversation under a new id,
-- leaving the original as it was.
ALTER TABLE sessions ADD COLUMN fork_of TEXT NOT NULL DEFAULT '';
