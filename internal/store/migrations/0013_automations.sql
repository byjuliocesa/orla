-- The automations the owner turned on: global ones with session '', the others with their
-- session. Orla starts them again when it starts.
CREATE TABLE automations_on (
  name    TEXT NOT NULL,
  session TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (name, session)
);
