-- Migration 1. Published migrations are never modified.

CREATE TABLE nodes (
  seq INTEGER PRIMARY KEY,
  id TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK (kind IN ('node', 'journal', 'day')),
  parent_seq INTEGER REFERENCES nodes(seq),
  position INTEGER NOT NULL,
  text TEXT NOT NULL,
  name_key TEXT,
  status TEXT CHECK (status IN ('open', 'done')),
  due TEXT,
  start_at TEXT,
  end_at TEXT,
  day TEXT NOT NULL,
  date_key TEXT GENERATED ALWAYS AS (coalesce(due, substr(start_at, 1, 10), day)) STORED,
  revision INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_source TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_source TEXT NOT NULL,
  trashed_at TEXT,
  trash_root INTEGER
);
CREATE UNIQUE INDEX nodes_siblings ON nodes(parent_seq, position);
CREATE INDEX nodes_roots ON nodes(position) WHERE parent_seq IS NULL;
CREATE INDEX nodes_name ON nodes(name_key) WHERE name_key IS NOT NULL;
CREATE UNIQUE INDEX nodes_days ON nodes(day) WHERE kind = 'day';
CREATE INDEX nodes_date ON nodes(date_key, seq);
CREATE INDEX nodes_updated ON nodes(updated_at, seq);
CREATE INDEX nodes_trash ON nodes(trash_root) WHERE trash_root IS NOT NULL;

CREATE TABLE types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  builtin INTEGER NOT NULL,
  color TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE node_types (
  node_seq INTEGER NOT NULL REFERENCES nodes(seq),
  type_id INTEGER NOT NULL REFERENCES types(id),
  PRIMARY KEY (node_seq, type_id)
) WITHOUT ROWID;
CREATE INDEX node_types_by_type ON node_types(type_id, node_seq);

CREATE TABLE aliases (
  node_seq INTEGER NOT NULL REFERENCES nodes(seq),
  alias TEXT NOT NULL,
  alias_key TEXT NOT NULL,
  PRIMARY KEY (node_seq, alias_key)
) WITHOUT ROWID;
CREATE INDEX aliases_by_key ON aliases(alias_key);

CREATE TABLE refs (
  source_seq INTEGER NOT NULL REFERENCES nodes(seq),
  target_seq INTEGER NOT NULL REFERENCES nodes(seq),
  PRIMARY KEY (source_seq, target_seq)
) WITHOUT ROWID;
CREATE INDEX refs_by_target ON refs(target_seq, source_seq);

CREATE TABLE revisions (
  node_seq INTEGER NOT NULL REFERENCES nodes(seq),
  revision INTEGER NOT NULL,
  action TEXT NOT NULL,
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  source TEXT NOT NULL,
  snapshot TEXT NOT NULL,
  PRIMARY KEY (node_seq, revision)
) WITHOUT ROWID;
CREATE INDEX revisions_by_time ON revisions(at, node_seq, revision);

CREATE TABLE requests (
  actor TEXT NOT NULL,
  request_id TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  response TEXT NOT NULL,
  at TEXT NOT NULL,
  PRIMARY KEY (actor, request_id)
) WITHOUT ROWID;

-- Derived, rebuildable full-text index. rowid is nodes.seq.
CREATE VIRTUAL TABLE search USING fts5(body, tokenize = 'unicode61 remove_diacritics 2');

-- Authentication state. One owner per instance.
CREATE TABLE owner (
  singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE oauth_clients (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  redirect_uris TEXT NOT NULL,
  created_at TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE oauth_codes (
  code_hash TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES oauth_clients(id),
  redirect_uri TEXT NOT NULL,
  challenge TEXT NOT NULL,
  scope TEXT NOT NULL,
  resource TEXT NOT NULL,
  expires_at TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE tokens (
  token_hash TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('access', 'refresh', 'personal')),
  client_id TEXT REFERENCES oauth_clients(id),
  name TEXT NOT NULL,
  scope TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT,
  last_used_at TEXT
) WITHOUT ROWID;
CREATE INDEX tokens_by_client ON tokens(client_id) WHERE client_id IS NOT NULL;
