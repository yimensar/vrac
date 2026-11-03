-- Vrac hosted format 1. Published migrations are immutable.
CREATE TABLE nodes (
 id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=32),
 parent_id TEXT REFERENCES nodes(id),
 position INTEGER NOT NULL,
 text TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 system TEXT UNIQUE,
 deleted INTEGER NOT NULL DEFAULT 0 CHECK(deleted IN (0,1)),
 CHECK(parent_id IS NULL OR parent_id<>id)
) STRICT;
CREATE INDEX nodes_by_parent_identifier ON nodes(parent_id,deleted,id);
CREATE INDEX nodes_by_context ON nodes(id,parent_id,system,deleted);
CREATE INDEX nodes_by_parent ON nodes(parent_id,deleted,position,id);
CREATE INDEX nodes_by_root_text ON nodes(text,position,id) WHERE parent_id IS NULL AND deleted=0 AND system IS NULL;
-- Derived navigation index: only active nodes with active children.
CREATE TABLE branches (
 node_id TEXT PRIMARY KEY NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
 parent_id TEXT REFERENCES nodes(id)
) STRICT;
CREATE INDEX branches_by_parent ON branches(parent_id,node_id);
CREATE TRIGGER nodes_branch_insert AFTER INSERT ON nodes BEGIN
 INSERT OR IGNORE INTO branches(node_id,parent_id)
  SELECT p.id,p.parent_id FROM nodes p WHERE p.id=new.parent_id AND p.deleted=0 AND new.deleted=0;
 INSERT OR IGNORE INTO branches(node_id,parent_id)
  SELECT new.id,new.parent_id WHERE new.deleted=0 AND EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=new.id AND child.deleted=0);
END;
CREATE TRIGGER nodes_branch_update AFTER UPDATE OF parent_id,deleted ON nodes BEGIN
 DELETE FROM branches WHERE node_id=new.id AND new.deleted=1;
 UPDATE branches SET parent_id=new.parent_id WHERE node_id=new.id;
 INSERT OR IGNORE INTO branches(node_id,parent_id)
  SELECT p.id,p.parent_id FROM nodes p WHERE p.id=new.parent_id AND p.deleted=0 AND new.deleted=0;
 INSERT OR IGNORE INTO branches(node_id,parent_id)
  SELECT new.id,new.parent_id WHERE new.deleted=0 AND EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=new.id AND child.deleted=0);
 DELETE FROM branches WHERE node_id=old.parent_id AND NOT EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=old.parent_id AND child.deleted=0);
END;
CREATE TRIGGER nodes_branch_delete AFTER DELETE ON nodes BEGIN
 DELETE FROM branches WHERE node_id=old.parent_id AND NOT EXISTS(SELECT 1 FROM nodes child WHERE child.parent_id=old.parent_id AND child.deleted=0);
END;
CREATE TABLE node_tags (
 node_id TEXT NOT NULL REFERENCES nodes(id),
 tag TEXT NOT NULL,
 PRIMARY KEY(node_id,tag)
) STRICT;
CREATE INDEX tags_by_tag ON node_tags(tag,node_id);
CREATE TABLE node_references (
 source_id TEXT NOT NULL REFERENCES nodes(id),
 token TEXT NOT NULL,
 target_id TEXT NOT NULL REFERENCES nodes(id),
 PRIMARY KEY(source_id,token)
) STRICT;
CREATE INDEX references_by_target ON node_references(target_id,source_id);
CREATE TABLE revisions (
 node_id TEXT NOT NULL REFERENCES nodes(id),
 revision INTEGER NOT NULL,
 at TEXT NOT NULL,
 summary TEXT NOT NULL,
 snapshot TEXT NOT NULL CHECK(json_valid(snapshot)),
 PRIMARY KEY(node_id,revision)
) STRICT;
CREATE TABLE activities (
 node_id TEXT PRIMARY KEY NOT NULL REFERENCES nodes(id),
 target_id TEXT NOT NULL REFERENCES nodes(id),
 target_revision INTEGER NOT NULL,
 journal_date TEXT NOT NULL,
 FOREIGN KEY(target_id,target_revision) REFERENCES revisions(node_id,revision) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE INDEX activities_by_date ON activities(journal_date,target_id);
CREATE INDEX activities_by_target ON activities(target_id,journal_date,node_id);
CREATE TABLE mutations (
 request_id TEXT PRIMARY KEY NOT NULL,
 kind TEXT NOT NULL,
 payload_hash TEXT NOT NULL,
 result TEXT NOT NULL CHECK(json_valid(result))
) STRICT;
-- rowid is only the local FTS implementation key, never a public identity.
CREATE VIRTUAL TABLE node_fts USING fts5(text,content='nodes',content_rowid='rowid',tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER nodes_fts_insert AFTER INSERT ON nodes BEGIN
 INSERT INTO node_fts(rowid,text) VALUES(new.rowid,new.text);
END;
CREATE TRIGGER nodes_fts_delete AFTER DELETE ON nodes BEGIN
 INSERT INTO node_fts(node_fts,rowid,text) VALUES('delete',old.rowid,old.text);
END;
CREATE TRIGGER nodes_fts_update AFTER UPDATE OF text ON nodes WHEN old.text<>new.text BEGIN
 INSERT INTO node_fts(node_fts,rowid,text) VALUES('delete',old.rowid,old.text);
 INSERT INTO node_fts(rowid,text) VALUES(new.rowid,new.text);
END;
PRAGMA application_id=1448235826;
PRAGMA user_version=1;
