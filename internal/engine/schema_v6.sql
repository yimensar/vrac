-- Migration 6. The owner signs in with a username, a password and a passkey.
-- Existing owners keep their password, are named "owner" and enroll a
-- passkey at their next sign-in. The owner columns are added by
-- migrateOwner, which skips existing ones.
UPDATE owner SET user_handle = randomblob(32) WHERE user_handle IS NULL;

CREATE TABLE IF NOT EXISTS passkeys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  credential TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_used_at TEXT
) WITHOUT ROWID;
