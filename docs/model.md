# Model and storage format

Vrac hosted V0 stores one current state in a server-local SQLite database.
`internal/engine/schema.sql` defines hosted format 1, application ID
1448235826. This is a different format from the Rust proof of concept; opening
an old or unrelated database is rejected. Never point the active database at
a synchronized folder or network share.

## Nodes and context

A node has a random 128-bit identity, rendered as 32 hexadecimal characters,
one optional parent, text, canonical tags, stable references and a revision.
Sibling order is deterministic. Numeric positions are private; callers use
first, last, before or after placement. The tree has no product depth limit.
Noninteractive operations can enumerate it; ordinary reads use bounded pages.

The protected root Journal has identity `journal`; protected days use
`day:YYYY-MM-DD` system keys. Opening an older day does not disrupt reverse
chronological ordering. Ordinary subjects can live at the root and can have
their own content/children.

Tags use lowercase Unicode letters/numbers, underscores and hyphens. They
classify only the node carrying them. Typing `#decision` extracts the tag
into canonical metadata. A hashtag inside a `[[reference]]` remains text.
A node may have up to 64 tags; empty content and content above 1 MiB are
rejected.

`[[Project Atlas]]` resolves an exact existing root label or atomically creates
a subject. Once bound, its stored target is a stable node ID. Subject renames
do not rewrite sources or retarget their existing tokens. Explicit
`[[32-character-node-id|label]]` can reference a chosen existing node without
label ambiguity. `reference_ids` can add explicit associations independently
of inline text. Labels are readable fallbacks; identity is authoritative.

A reference on an ancestor supplies context to its descendants. Project
search returns that contextual branch and related activity. This is a derived
query over the canonical tree and references, not copied tags on descendants.
A tag filter applies to the result node itself.

## Current state and history

An evolving decision retains one node and current revision. Content/tag/link
updates, a revision snapshot and a linked activity bullet are committed in one
transaction. No-op content updates add no revision or activity. Editing an
activity bullet does not recursively create an activity-of-activity trail.

Activity bullets retain explicit stable references to the changed note and
its subjects/ordinary ancestors at the time of the change. Move events retain
both old and new context, so earlier project activity stays discoverable after
a note moves or its parent references change. These are event facts, not
propagated tags or a cache of the current tree.

`from`/`to` search filters refer to journal placement. `activity_from` and
`activity_to` select notes worked on in that period. Bounds are inclusive ISO
calendar dates. The server supplies today's date in its configured timezone;
engine callers supply explicit dates when a specific timezone matters.

A restore creates a new current revision from a historical snapshot, keeping
the current tree location and restoring the stable reference targets. It
preserves the history of the restore itself. Deletion archives a leaf; nonempty
or ordinarily referenced nodes must first be reorganized. Archived revisions
remain recoverable and exportable.

## Atomicity and concurrency

Mutations use explicit SQLite transactions, foreign keys, WAL and synchronous
FULL. `expected_revision` is mandatory for edits/moves/restores and detects
concurrent changes. A request ID durably records its payload and result: an
identical retry returns the original result; reuse with another payload fails.
Use a new request ID for a newly intended change, and retain the original ID
when retrying after an uncertain response.

Search uses a derived FTS5 index. Full-text words are combined with AND and
prefix matching; tag, date and subject filters are exact. Pagination uses
query-bound opaque cursors and deterministic order. Cursors are not a snapshot
across intervening mutations; refresh a view after changing its ordering.

## Recovery and evolution

`vrac export` writes versioned UTF-8 JSON with streamed arrays for nodes,
tags, references, revisions, activities and mutation receipts. Identity, order,
history and retry behavior survive round-trip import. `vrac import` accepts
only an empty hosted Vrac database and validates canonical integrity before
commit. A failed import leaves the empty database intact.

Backups use a consistent SQLite snapshot and validate it before publishing a
new destination. Exports and backups do not overwrite an existing file. Keep
copies off-host and rehearse restore/import into another directory.

The FTS index and internal-branch navigation helper are derived and
rebuildable with `vrac reindex`. The helper indexes nodes with children and
is maintained without rewriting subtree descendants. Future schema changes must
increase user_version with explicit tested migrations; a published format or
migration is immutable. Source text and canonical links remain recoverable
without an embedding provider, AI subscription, or particular web editor.
