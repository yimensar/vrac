# Vrac — V1 specification

Status: V1 implemented on 2026-11-05; open questions in section 10.

## 1. Vision

Vrac is a self-hosted personal workspace for notes, subjects, projects, tasks,
meetings and events. It sits between Workflowy (one infinite outline, zoom on
anything), Logseq (the daily journal as the default place to write) and Tana
(typed nodes that show up wherever they matter).

What it adds: **Vrac is the memory, the user's own AI is the brain.** Any AI
client (Claude, ChatGPT, Codex…) connects through MCP, using the subscription
the user already pays for. With a Vrac skill, the AI records tasks, decisions
and context after each of its actions (reading mail, a meeting transcript, an
analysis) without being asked to "organize the notes". The user writes as
little as possible and never designs a system.

The web interface is secondary but complete: without any AI, the user must be
able to find everything, for example the open tasks of project Northwind or the
latest decisions on project Globex.

### Principles

1. **Write first, organize never.** The journal of the day is always the
   default place to write. Structure comes from types and references, not
   from choosing a location.
2. **Conventions, not configuration.** Vrac ships the useful types with fixed
   fields. There are no schemas, properties or views to design.
3. **One query, two interfaces.** The UI and MCP use the same query engine.
   Whatever the AI can find, the user can find, and the reverse.
4. **Nothing is created implicitly.** New types and new referenced nodes need
   an explicit choice, by the user in the UI or by the AI with the user's
   confirmation. This prevents duplicates such as Globex / OXA / globex-project.
5. **Every write is attributed and reversible.** The user can see what an AI
   wrote, from which source, and undo it.
6. **The user owns the data.** Self-hosted, one data directory, complete
   export.

## 2. Model

### 2.1 Node

Everything is a node. There is no separate concept of a page: a "page" is a
node the user zoomed into. Any node can be zoomed, referenced and typed.

A node has:

- an opaque, stable ID that survives renames, moves, export and import;
- one parent (or none) and a private position among its siblings;
- text (single line, inline Markdown);
- zero or more types, and the fields those types define;
- zero or more aliases (alternative names used for resolution and search);
- a revision number, creation and update times, and the actor and source of
  its creation and last update.

The children of a node are its content: the notes of a meeting are the
children of the meeting node.

### 2.2 Where nodes live

- **Journal.** One protected day node per local date, created on demand under
  a protected Journal root. Writing happens there by default.
- **Where written.** A node lives where the user or the AI wrote it: in a day,
  or under any other node.
- **Root.** A node created from a reference (`[[Dupont]]` when Dupont does not
  exist) has no parent. It lives at the root.

A node's location never limits where it appears: types and references make it
visible everywhere it matters.

### 2.3 Types: `#` says what a node is

A type changes what a node is. Typed nodes get a color, an icon and the type's
fields. A node may carry several types (`#task #decision`).

**A node's types are exactly the `#type` tokens of its text.** There is no
hidden type: a token stays where it was written, like a reference, so
sentences read naturally (`#meeting with [[Alice]] about [[Globex]]`), and
removing it removes the type. A type given outside the text, such as
"Create Dupont as person", is written into it (`Dupont #person`).

One rule tells labels from words: **a `#type` at the end of the line labels
the node; a `#type` inside the sentence is part of it.** A reference shows its
target as written, without the trailing labels: `[[Northwind]]` to `Northwind #project`
shows Northwind, and a reference to `#meeting with [[Bruno]] about [[Northwind]]`
shows "#meeting with Bruno about Northwind", the type in its color.

Built-in types (fixed fields, no configuration):

| Type | Meaning | Fields |
|---|---|---|
| `#task` | something to do | status (open, done), optional due date |
| `#meeting` | an exchange with people, with notes | optional date and time; referenced `#person` nodes are participants |
| `#event` | something happening: an event, a trip | start date or date-time, optional end |
| `#decision` | something decided | none; its date is its journal date |
| `#project` | a project or subject followed over time | none |
| `#person` | someone | none |

Free types: the user can create a type such as `#reading` or `#idea`. A free
type has a color and a type view listing its nodes, but no fields. Creating
one is always explicit (see 2.7).

**Types are not inherited.** A task written under a meeting is not a meeting.

### 2.4 References: `[[ ]]` says what a node is about

`[[Northwind]]` is a link to a node by ID. The displayed label is the target's
current name, so renaming never breaks references. The old name may remain as
an alias.

`[[Name|ID]]` is the readable form of a canonical reference: AI clients and
the editor send it so that names never have to be guessed. Plain `[[Name]]`
resolves only when exactly one node has that name or alias.

**References are inherited.** See 2.5.

### 2.5 Context: what a node concerns

A node concerns X when X, or a descendant of X, is the node itself, one of its
ancestors, or referenced by the node or one of its ancestors. Journal day
nodes are time, not subjects, and never count as context.

Examples:

- a task under `Copil [[Northwind]] #meeting` concerns Northwind without being tagged
  again;
- a task under the node Northwind concerns Northwind;
- a task referencing `[[Lot 2]]`, a child of Northwind, concerns Lot 2 and Northwind.

Context is maintained at write time in a derived, rebuildable index that
lists, for every node, the subjects it concerns, with the node's date. A
subject's nodes are then read in date order and context queries stay fast
on large workspaces. Writing a node or changing its references updates its
subtree; moving a node also updates the nodes that reference it, which is
the rare costly operation. Renames change nothing. A property test checks
the index against the definition after random operations.

### 2.6 Hierarchy

There are two hierarchies, both built automatically:

- **Type to instances.** The view of a type lists every node carrying it. The
  `#project` view is the list of all projects.
- **Subject to sub-subjects.** This is simply the tree. A `#project` nested
  under another `#project` is a sub-project. `[[Northwind/Lot 2]]` is a shortcut
  that creates (or finds) Lot 2 as a child of Northwind.

Input shortcut kept from Memos habits: `#project/Northwind` is a reference to
Northwind; when Northwind is created through it, it carries the type `#project`. An
existing Northwind is referenced as is. The stored model stays clean.

### 2.7 Creation rules

- **UI.** Typing an unknown `#name` or `[[name]]` makes autocomplete offer to
  create it; creation happens only when that option is chosen. Creating a
  referenced node offers its type in the same step: "Create Dupont as: person
  · project · no type". This prevents `Lunch with [[Dupont]] #person` from
  typing the lunch instead of Dupont. Writing `Northwind #project` when a project
  named Northwind already exists offers to reference it instead.
- **AI.** A write that names an unknown type or node is rejected. The error
  returns close candidates, aliases included. The AI either reuses one or
  creates explicitly, after confirming with the user as its skill instructs.

### 2.8 Dates in text

A date phrase in a task, meeting or event ("Friday", "vendredi", "14h",
"2026-11-12") is converted on input into the corresponding field, relative to
the day being written, and shown as a chip. The field is canonical, not the
text. V1 understands English and French phrases and ISO dates.

### 2.9 History and deletion

Every change creates a revision and an activity entry (actor, source, time).
Concurrent edits check the expected revision. Retried mutations use durable
request IDs and reject changed payloads. Deleting moves a node to a trash
from which it can be restored.

## 3. Daily scenarios

1. **Create a project on the fly.** In today's journal: `Northwind #project`. The
   node is born in the day, typed project, and appears in the `#project`
   view. In outlines, projects and people are shown collapsed; their content
   is on their own page.
2. **Create from a reference.** `Call with Marc about [[Northwind]]` with no Northwind
   yet: autocomplete offers "Create Northwind as: project · person · no type". The
   node is created at the root and the journal line references it.
3. **Mention an existing project.** `[[Su` → autocomplete → Northwind.
4. **A meeting.**
   ```
   - Copil [[Northwind]] with [[Marc]] 14h #meeting
     - Lot 2 is moved to January #decision
     - Follow up with Marc on the schedule Friday #task
   ```
   The decision and the task concern Northwind and Marc. They appear on Northwind, on
   Marc, in the agenda and in tasks.
5. **Zoom into Northwind.** At the top: what was written under Northwind (description,
   links, notes), as a normal outline. Below, gathered by Vrac: open tasks,
   upcoming events and meetings, recent decisions, then every mention, day by
   day.
6. **A sub-project.** In Northwind, write `Lot 2 #project` as a child, or
   `[[Northwind/Lot 2]]` from anywhere.
7. **The AI processes mail.** Marc asks for the revised estimate for Northwind by
   Friday. Claude searches "Northwind", finds the node, and writes in today's
   journal: `Send the revised estimate to [[Marc]] [[Northwind]] Friday #task`,
   attributed to Claude, source Outlook. If nothing matches, it proposes a
   creation and asks the user.
8. **No AI available.** Command palette → "Northwind" → open tasks at the top. Or
   the `#project` view with every project, its open tasks and last activity.
   Or search with the chips `#decision` + `[[Globex]]`, sorted by date.
9. **Rename.** Northwind becomes Northwind Traders; every reference follows.

## 4. Query

One query engine serves the UI and MCP. A query combines:

- text (full-text over node text and aliases);
- types;
- context: concerns one or more nodes (2.5);
- task status;
- a date range over the node's date (due date for tasks, start for meetings
  and events, journal date otherwise);
- actor or source;
- sort (date, recency of update) and an opaque cursor for pagination.

Examples:

- open tasks on Northwind: `#task` · concerns Northwind · status open;
- latest decisions on Globex: `#decision` · concerns Globex · sorted by date.

Results are deterministic and bounded. Normal operations never traverse the
whole tree.

## 5. Interface

React, TypeScript and shadcn/ui, built as a single-page app that uses only
the public HTTP API, the same one any other client would use. Usable on
mobile browsers.

The app on phones and computers is a Progressive Web App: installable from the
browser, with its own window and icon, no store and no separate codebase. The
manifest declares a share target so that, where the platform supports it,
text and links can be shared to Vrac and land in today's journal.

A native shell is not planned. If a need appears that a PWA cannot meet, such
as a system-wide quick-capture shortcut, it would wrap the same UI and
authenticate through the built-in OAuth server with PKCE. Offline editing is a
non-goal; it would require synchronization and a different model.

- **Today**: agenda of the day, due and overdue tasks, the day's journal.
- **Journal**: days in sequence, outline editor.
- **Node zoom**: authored outline, then the generated sections (scenario 5).
- **Type views**: a table per type (`#project` with open tasks and last
  activity; `#person`; `#meeting`…).
- **Tasks**: open tasks grouped by due date, filterable by context.
- **Agenda**: local calendar, week and month, from meetings and events.
- **Search and command palette**: query chips (type, context, status, date),
  jump to any node.
- **Activity**: what changed, by whom and from which source, with undo.

**A row is the node itself.** Wherever a node appears with its bullet (a
journal day, a page, today's lists, the agenda, tasks, a subject's sections,
search results, a page title) it is edited in place, with its children, and
reads exactly as it is written: references and types are atoms, rendered
the same while reading and while editing (ProseMirror), and a click places
the caret where it lands. Outline keys: Enter splits, Tab and Shift+Tab
indent and outdent, Backspace deletes an empty row or merges into the row
above, arrows move between rows, Alt+Shift+arrows reorder, Cmd+Enter
toggles a task. In lists of unrelated nodes the listed rows stay in place
and new rows go inside them.

The agenda and upcoming sections show only meetings and events with an
explicit date; a meeting noted in the journal without a time is not
scheduled.

Preferences live on the server, so they follow the owner across devices:
how outlines open (projects and people collapsed, everything expanded or
everything collapsed), the theme (system, light, dark) and whether typed
dates become fields. Each outline can also be expanded or collapsed at
once.

## 6. MCP

Streamable HTTP at `/mcp`. Server instructions explain Vrac's conventions to
any client: write in today's journal by default, search before creating,
reference by ID, use built-in types, never create implicitly.

Tools (V1):

| Tool | Purpose |
|---|---|
| `vrac_today` | date, time zone, today's journal, agenda, due tasks, types |
| `vrac_search` | the query of section 4 |
| `vrac_resolve` | names to candidate nodes, aliases included |
| `vrac_get` | a node, its bounded children and its generated sections |
| `vrac_day` | a journal day |
| `vrac_types` | built-in and free types |
| `vrac_write` | create node trees, by default in today's journal |
| `vrac_update` | text, types, fields or aliases, with the expected revision |
| `vrac_move` | change parent or order |
| `vrac_trash` | move to the trash, or restore |
| `vrac_create_type` | create a free type explicitly |
| `vrac_activity` | recent changes with actor and source |

Results show text with `[[Name|ID]]` references.

Every mutation takes a request ID and a source (for example `outlook`); the
actor comes from the authenticated client.

### Skill

The repository ships a `vrac` skill for Claude (and equivalents where
supported). It tells the AI when to write (after triaging mail or messages, a
meeting, a decision, an analysis) and how: search and resolve first, reuse
existing nodes, reference subjects, type what is typed, confirm creations with
the user, always set the source. It replaces the user's current Memos skill.

## 7. Hosting and authentication

- One user per instance. One instance is one database; nothing assumes global
  state, so a future hosted offer can run one database per subscriber.
- The owner signs in with a username and a password (argon2id), then a
  passkey (WebAuthn): a password alone opens nothing. First start offers
  `/setup`, which creates the account with its first passkey; Settings adds
  or removes passkeys, keeping at least one.
- Optionally, sign-in through an identity provider (OIDC, such as Pocket ID)
  replaces both: authorization code with PKCE, a browser-bound state, a
  nonce, and only the subjects or verified emails listed in the
  configuration.
- Recovery happens on the server: `vrac password [-user NAME]` sets the
  password (creating the owner if none) and `vrac passkeys reset` removes
  passkeys; the next sign-in then enrolls a new one.
- Built-in OAuth 2.1 authorization server with dynamic client registration and
  PKCE, so connecting Claude or ChatGPT only needs the instance URL.
- Personal access tokens for CLIs and scripts.
- TLS terminates at the user's reverse proxy.

## 8. Storage and durability

- Go, one binary that serves the API, MCP and the compiled web UI, packaged
  by the Nix flake (`nix run`); configuration limited to the public URL, the
  data directory and the time zone. A Docker image comes with the first
  deployment.
- SQLite is the source of truth: WAL, FULL synchronization, foreign keys,
  explicit transactions for every mutation. Full-text and context indexes are
  derived and rebuildable.
- Migrations run automatically at start, after a SQLite snapshot backup.
- Backups use SQLite's backup facilities, never a raw file copy.
- Versioned JSON export and import (into an empty instance), preserving IDs,
  structure, types, references and history. It is also the path between
  self-hosted and hosted.

## 9. Scope

V1: everything above, except the Docker image.

Later: quick capture desktop app (global shortcut), audio capture and
transcription, inbox of unfiled captures, attention queue (overdue, stale
projects, meetings without follow-up), MCP prompts (daily briefing, weekly
review, meeting prep), external calendars, Reminders and Todoist bridges,
public sharing of a node or a form, the hosted offer.

## 10. Open questions

1. **Home of nodes created in the journal.** A project born in a day stays in
   that day, and what is written under it later lives there too. Keep it, or
   add a "move to root" action, or move typed entities automatically?
2. **Evolving decisions.** Show the latest first only, or link a decision to
   the one it replaces?
3. **What `[[ ]]` can target.** Resolved: any node. Completion matches names
   and aliases first, then the words of any node (meetings, tasks).
4. **Memos import** in V1 (including `#a/b` tags through the 2.6 shortcut)?
5. **License.** AGPL-3.0 is proposed to protect a future hosted offer.
