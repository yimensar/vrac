-- Migration 4. The context index: the subjects each node concerns, with the
-- node's date so a subject's nodes are read in date order. Derived and
-- rebuildable; see context.go.
CREATE TABLE IF NOT EXISTS concerns (
  subject_seq INTEGER NOT NULL REFERENCES nodes(seq),
  node_seq INTEGER NOT NULL REFERENCES nodes(seq),
  date_key TEXT NOT NULL,
  PRIMARY KEY (subject_seq, node_seq)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS concerns_by_date ON concerns(subject_seq, date_key, node_seq);
CREATE INDEX IF NOT EXISTS concerns_by_node ON concerns(node_seq);
