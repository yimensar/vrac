# Engine integration contract

The engine owns canonical SQLite state, transactions, queries and validation.
Callers use its synchronous Go API; adapters contain no SQL. Build with CGO and
`-tags sqlite_fts5`. Errors wrap `ErrNotFound`, `ErrConflict`, `ErrInvalid` or
`ErrProtected`; storage failures remain observable errors.

- `Open(path)`, `Close()`, `Journal()`, `EnsureDay(date)`
- `Get(id)`, `Children(parentID,cursor,limit)`, `Path(id)`
- `Read(ids,includeChildren,limit)` reads up to ten views in one snapshot.
- `Create(CreateInput)` creates one nested branch atomically.
- `Capture(date,inputs,requestID,fallbackDate...)` captures several roots atomically.
- `Update(id,UpdateInput,fallbackDate...)`, `Move(id,MoveInput,fallbackDate...)`
- `Delete(id,expectedRevision,date,requestID,fallbackDate...)` archives a leaf.
- `Restore(id,revision,expectedRevision,date,requestID,fallbackDate...)` restores
  content, tags and stable references into a new revision at the current location.
- `Search(SearchFilters)`, `Backlinks(id,cursor,limit)`
- `TagsPage(prefix,cursor,limit)`, `Tags(prefix,limit)`
- `Revisions(id,cursor,limit)`, `ReadRevision(id,revision)`
- `Export(writer)`, `Import(reader)`, `Backup(destination)`, `Check()`
- `RebuildSearch()` rebuilds both FTS and the derived internal-branch index.

The JSON types are in `model.go` and `views.go`. Collection pages contain at
most 100 entries. Cursors are opaque and bound to their query; no collection
uses OFFSET. Reads containing several queries use one database snapshot.

## Capture, references and tags

Capture returns its root nodes; descendants are available through Children.
Inline `#tags` become canonical lowercase tag properties and are removed from
stored text. Explicit tags are combined with inline tags. A nil update Tags
slice preserves the current tags, while an empty slice replaces them.

`[[label]]` resolves an exact ordinary root label or creates a subject; ISO
dates resolve protected Journal days. `[[id]]` and `[[id|fallback label]]` bind
an existing identity. Existing unchanged tokens retain their stable target
through target renames. Reference reads include the current target label and
the source token. A nil ReferenceIDs slice preserves non-inline associations;
an empty slice clears them. Removing an inline token removes that binding.

## Current knowledge and historical activity

Editing a decision updates the same identity. Each meaningful mutation records
a revision; a normal search returns current nodes, with historical snapshots
available separately. An activity is an ordinary Journal note tagged `activity`
and linked to the exact edited node/revision through the activities relation.

Its references capture the identities known at that time: the edited node,
then-current references of its ancestors, and ordinary ancestor identities.
Protected Journal/day ancestors are excluded. These are canonical event data,
not inherited labels or derived metadata. Earlier activities retain their
associations when the source node moves or its references change. A move's own
activity captures both the previous and destination contexts. Content/reference
updates similarly retain old and new direct references in the activity.

Journal From/To filters describe current tree placement. ActivityFrom/To selects
current target nodes with recorded work in that event-date interval. To inspect
historical project work, search Journal activity notes by subject and date.

Dates are ISO calendar dates. An empty mutation date is resolved inside its
transaction after idempotency lookup, using the optional adapter fallback date
or UTC today. The original caller payload remains the receipt identity: a retry
after midnight returns the original result. Reusing a request ID with changed
payload fails. Updates, moves and restoration require the expected revision.

## Storage and recovery

The hosted format has its own application ID and user_version 1. Other formats,
including the former Rust POC, are rejected before changing journal mode.
Connections enable foreign keys, WAL and FULL synchronization. New database
files have mode 0600. No mutation weakens durability for performance.

Only canonical tables enter versioned UTF-8 JSON exports. Import requires a
fresh database, streams rows in one transaction, rebuilds derived indexes and
validates integrity before commit. Backup uses a SQLite snapshot, integrity
validation, checkpoint, file synchronization and no-overwrite publication.

The internal-branch index contains only nodes with active children. Triggers
maintain old/new parent membership and a moved branch's own parent; descendants
are never rewritten. A subject scope expands internal branches, then matches
leaf children through their parent. Untagged subject browsing merges bounded
ordered streams, including at most one page of children per matching parent.
The global first N cannot contain a child beyond its parent's first N; seeking
before each source limit preserves complete, duplicate-free ID pagination.
