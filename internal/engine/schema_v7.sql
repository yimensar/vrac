-- Migration 7. Tags are the user's own: no type is built in, and a tag may
-- carry one trait, "to-do", which gives its nodes a checkbox. Any node may
-- have a checkbox (status) and a date (start_at) with an optional end
-- (end_at), whatever its tags. The former due date moves into start_at
-- and the due column is retired (always NULL; date_key still reads it
-- first, which is harmless).
-- The todo column is added by migrateTags, which skips it when present.
UPDATE types SET todo = 1 WHERE name = 'task' AND builtin = 1;
UPDATE types SET builtin = 0;
-- Where both were set the start wins; the context index copies date_key.
UPDATE concerns SET date_key = (SELECT substr(n.start_at, 1, 10) FROM nodes n WHERE n.seq = concerns.node_seq)
  WHERE node_seq IN (SELECT seq FROM nodes WHERE due IS NOT NULL AND start_at IS NOT NULL);
UPDATE nodes SET start_at = due WHERE start_at IS NULL AND due IS NOT NULL;
UPDATE nodes SET due = NULL WHERE due IS NOT NULL;
-- Types nobody used, such as the former built-in ones, disappear.
DELETE FROM types WHERE id NOT IN (SELECT type_id FROM node_types);
