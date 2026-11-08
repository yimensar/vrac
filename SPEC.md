# Vrac — V1 specification

Status: V1 implemented on 2026-11-05; model made generic on 2026-11-07 (tags without fields); open questions in section 10.

## 1. Vision

Vrac is a self-hosted personal outliner for notes, to-dos, meetings,
decisions, people, projects, whatever the user tags. It sits between
Workflowy (one infinite outline, zoom on anything), Logseq (the daily
journal as the default place to write) and Tana (tagged nodes that show up
wherever they matter).

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
2. **Conventions, not configuration.** Tags are the user's own words, with
   no fields to design; the two properties every node may carry, a checkbox
   and a date, are the same everywhere. There are no schemas or views to
   build.
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
- zero or more tags;
- an optional checkbox (open or done) and an optional date, a day or a day
  and time, with an optional end: a deadline, a meeting time, an event;
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

A node's location never limits where it appears: tags and references make it
visible everywhere it matters.

### 2.3 Types: `#` says what a node is

A tag says what a node is. Tagged nodes get a color and a computed page. A
node may carry several tags (`#task #decision`).

**A node's tags are exactly the `#tag` tokens of its text.** There is no
hidden tag: a token stays where it was written, like a reference, so
sentences read naturally (`#meeting with [[Alice]] about [[Globex]]`), and
removing it removes the tag. A tag given outside the text, such as
"Create Dupont as person", is written into it (`Dupont #person`).

One rule tells labels from words: **a `#tag` at the end of the line labels
the node; a `#tag` inside the sentence is part of it.** A reference shows its
target as written, without the trailing labels: `[[Northwind]]` to `Northwind #project`
shows Northwind, and a reference to `#meeting with [[Bruno]] about [[Northwind]]`
shows "#meeting with Bruno about Northwind", the tag in its color.

**Tags are the user's own.** Nothing is built in: a new instance has no tag,
and someone who wants `#book` and `#idea` never meets `#project` or
`#person`. A tag has a name and a color, and no fields. It carries one
optional trait: **to-do**, which makes the nodes written with it to-dos.
That is what makes `#task` behave like a task without configuring anything
else; the trait is chosen when the tag is created, or later on its page.

**The checkbox is the to-do tag.** A node is a to-do if and only if it
carries a to-do tag; its checkbox is open or done. `/todo` and ⌘Enter on
any line write the tag at its end, "Not a to-do" removes it, and the
checkbox comes and goes with the tag, so the text stays the truth and
everything about tasks (lists, searches, a future task system) hangs on
one tag. Giving a tag the trait opens the nodes carrying it; removing it
closes them.

**Any node may carry a date**, whatever its tags: a day (`2026-11-12`) or
a day and time (`2026-11-12T14:00`), with an optional end: a deadline, a
meeting time, an event.

**Tags are not inherited.** A to-do written under a meeting is not a meeting.

**Text is a line, with inline Markdown.** A node is one short line; its
details are its children. The line may hold `**bold**`, `` `code` ``,
`[text](url)` and bare URLs, rendered while reading and styled in place
while editing, so a row reads the same both ways; lines between ``` fences render as a
code block. A node may hold several
lines (Shift+Enter in the editor, a newline for a client): runs of spaces
are normalized within a line, blank lines dropped, and the first line
names the node. Lines starting with `|` render as a Markdown table, the
one shape for comparisons. Headings, lists and numbering do not exist:
the outline is the structure.

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

- **Tag to nodes.** The page of a tag lists every node carrying it. The
  `#project` page is the list of all projects, if the user has such a tag.
- **Subject to sub-subjects.** This is simply the tree. A `#project` nested
  under another `#project` is a sub-project. `[[Northwind/Lot 2]]` is a shortcut
  that creates (or finds) Lot 2 as a child of Northwind.

Input shortcut kept from Memos habits: `#project/Northwind` is a reference to
Northwind; when Northwind is created through it, it carries the tag `#project`. An
existing Northwind is referenced as is. The stored model stays clean.

### 2.7 Creation rules

- **UI.** Typing an unknown `#name` or `[[name]]` makes autocomplete offer to
  create it; creation happens only when that option is chosen. A new tag is
  offered plain or as a to-do tag. Creating a referenced node offers it as
  is: `Lunch with [[Dupont]] #person` types the lunch, not Dupont, so Dupont
  is created plain and tagged on its own line when wanted.
- **AI.** A write that names an unknown tag or node is rejected. The error
  returns close candidates, aliases included. The AI either reuses one or
  creates explicitly, after confirming with the user as its skill instructs.

### 2.8 Dates in text

Text is never parsed for dates by itself. A date is set from the line with
`/due friday 14h` (or `/date`; `/due` alone offers today, tomorrow and
next Monday), from a node's page, or by the AI with `date` or `date_text`.
A phrase is read in English or French, the way task tools read them:
casual words ("today", "tomorrow", "yesterday", "tonight", "demain",
"hier", "ce soir"), amounts ("in 3 days", "dans une semaine", "2 weeks
ago", "il y a 2 jours"), periods with a modifier ("next week" is next
Monday, "end of week" is Friday, "end of month", "fin du mois", "next
month", "this weekend"), weekdays with a modifier ("friday", "fri", "last
friday", "vendredi dernier", "this thursday" allows today), dates by name
or number ("12 novembre", "nov 12", "12/11", "2026-11-12", looking ahead
for a day of the year already past) and a time ("14h30", "5:30pm", "at
9:15", "noon", "minuit"; "tonight" alone means 20:00). It is read
relative to the day being written when a node is created, and to today
when a date is set later on an existing node, and stored as the canonical
field, shown as a chip while reading and while editing. A phrase not
understood is refused and the line keeps its text. `/no date` clears it.

### 2.9 History and deletion

Every change creates a revision and an activity entry (actor, source, time).
Concurrent edits check the expected revision. Retried mutations use durable
request IDs and reject changed payloads. Deleting moves a node to a trash
from which it can be restored.

## 3. Daily scenarios

1. **Create a project on the fly.** In today's journal: `Northwind #project`. The
   node is born in the day, tagged project, and appears on the `#project`
   page. Its content is on its own page.
2. **Create from a reference.** `Call with Marc about [[Northwind]]` with no Northwind
   yet: autocomplete offers "Create Northwind". The node is created at the
   root and the journal line references it.
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
   links, notes), as a normal outline. Below, gathered by Vrac: what concerns
   Northwind from elsewhere, one list per tag (`#task` 12, `#decision` 3,
   `#meeting` 5) and the untagged lines, most recent first.
6. **A sub-project.** In Northwind, write `Lot 2 #project` as a child, or
   `[[Northwind/Lot 2]]` from anywhere.
7. **The AI processes mail.** Marc asks for the revised estimate for Northwind by
   Friday. Claude searches "Northwind", finds the node, reads the user's tags
   and writes in today's journal: `Send the revised estimate to [[Marc]]
   [[Northwind]] #task` dated Friday, attributed to Claude, source Outlook.
   If nothing matches, it proposes a creation and asks the user.
8. **No AI available.** ⌘K → "Northwind" opens its page with its lists,
   its open to-dos among them; ⌘K → `#task` lists every open to-do. Or the `#project` page.
   Or search with the chips `#decision` + `[[Globex]]`, sorted by date.
9. **Rename.** Northwind becomes Northwind Traders; every reference follows.

## 4. Query

One query engine serves the UI and MCP. A query combines:

- text (full-text over node text and aliases);
- tags, or no tag at all;
- context: concerns one or more nodes (2.5), optionally outside their own
  outlines (what refers to them from elsewhere);
- checkbox state;
- dated only, and a date range over the node's date (its own date when it
  has one, its journal day otherwise);
- actor or source;
- optionally, for each result, the typed nodes it concerns, most specific
  first, so a client can group results by project without repeating the
  context on each node;
- sort (date, recency of update) and an opaque cursor for pagination.

Examples:

- open to-dos on Northwind: concerns Northwind · status open;
- latest decisions on Globex: `#decision` · concerns Globex · sorted by date;
- this week: dated · from Monday to Sunday.

Results are deterministic and bounded. Normal operations never traverse the
whole tree.

## 5. Interface

React, TypeScript and shadcn/ui, built as a single-page app that uses only
the public HTTP API, the same one any other client would use. Usable on
mobile browsers. On screen the product is called `vrac.`, with the dot; the
repository, the binary and the API keep the name `vrac`.

**One page to write.** The home is today's journal and nothing else: no
lists above or below it, an empty page to write on. On a keyboard it opens
with the caret in today's new line, and typing anywhere on the page lands
there. The journal view shows today at the top, then the previous days as
you scroll, today still written in place. To-dos are not gathered on the
page: they are written in today's journal whatever their horizon (a date
on one is a real deadline) and found again by search, on the `#task` tag
page, or on the page of what they concern. A free outline cannot tell
which of a to-do's contexts is its subject (a meeting, the project the
meeting is about, a person it mentions), so no view guesses one. There is
no permanent navigation: two quiet
icons float at the top right, the search (⌘K) and a menu to the other
views, and the writing line itself holds every power through completion
menus:

- `#` offers the tags, and creating one, plain or as a to-do tag;
- `[[` or `@` opens at once on the subjects in use (the referenced ones,
  most recently referenced first, then typed nodes), then offers nodes
  matching what is typed, never the row itself, and creating one: `[[Dupont #person` creates Dupont tagged person in one
  go, and plain "Create" asks which tag, or none, next;
- `/` offers everything a row can become or do: the tags, To do (one
  entry per to-do tag, or creating `#task` as the first), Done or Reopen,
  Not a to-do, a date from a phrase (`/due friday 14h`), No date, Bold,
  Code, Code block, Link, Table (`/table` offers sizes, `/table 3x2` lays
  out columns × rows), Zoom in, Delete. The same formatting has keys: ⌘B
  bold, ⌘E code, ⌘⇧E code block, ⌘⇧K link; each wraps the selection, or
  opens its markers with the caret inside.

A row reads as it will be saved: a tag being typed colors the bullet at
once and a to-do tag opens the checkbox before anything is saved. The new
line at the end of an outline behaves like a row that does not exist yet:
Tab puts it under the row above, Shift+Tab brings it back, Backspace
joins the row above.

On touch screens, where Tab and Alt+Shift+arrows do not exist, a bar above
the keyboard offers indent, outdent, move up and down, done and zoom while
a row is edited.

The app on phones and computers is a Progressive Web App: installable from the
browser, with its own window and icon, no store and no separate codebase. It
fills the screen around the notch and the home indicator, colors the system
bars with the current theme, and ships a full-bleed icon for Android masks.
The manifest declares a share target so that, where the platform supports
it, text and links can be shared to Vrac: the shared title, text and link
become one line, without repeating a part another one contains, which one
tap or Enter adds to today's journal with the source `share`.

A native shell is not planned. If a need appears that a PWA cannot meet, such
as a system-wide quick-capture shortcut, it would wrap the same UI and
authenticate through the built-in OAuth server with PKCE. Offline editing is a
non-goal; it would require synchronization and a different model.

- **Today** (home): today's journal alone, outline editor.
- **Journal**: today, then the days in sequence as you scroll.
- **Node zoom**: breadcrumb from the journal (Escape goes up one level),
  the node as title, its outline, then one list per tag of what concerns
  it from elsewhere (scenario 5). Nothing else: its date, other names and
  history are reached from the palette and Activity.
- **Tag pages**: every node carrying the tag, most recent first; a to-do
  tag shows open ones then done ones; the to-do trait is toggled there.
- **Agenda**: everything dated, day by day.
- **Search**: query chips (tag, context, status, date).
- **Command palette** (⌘K), the power box: one field that searches
  everything (nodes, tags, views, commands, settings) and runs what it
  finds; it opens on the subjects in use. The first letter is asked of
  the server at once, the next ones after a pause in typing, and the
  last answer stays shown until the next. Enter runs a result's default action (open the node, the tag
  page, the view); → opens its own menu of actions in place, searched by
  the same field, with the path shown as chips before the text; Backspace
  or ← in an empty field goes back up, and a footer names the keys. A
  node's menu: open, mark done or reopen, set or change the date (today,
  tomorrow, in a week, or a phrase typed in the field), add a line under
  it (the field is the line), its other names (the field is the list),
  search what concerns it, move to trash with undo. A tag's menu: its page, the search with the tag as a chip, write a
  line carrying it, to-do tag or plain. A setting's menu: its options.
  Commands: write in today's journal (the field is the line), create a
  tag (plain or to-do), go to a day (the field is a phrase); `#name` of a
  tag that does not exist offers to create it directly, and a date phrase
  typed at the root ("yesterday", "next monday", "12 nov") opens that day
  of the journal, whether it has content or not.
- **Activity**: what changed, by whom and from which source, with undo.

There are no other pages: tags, projects, people and settings are reached
from the palette, and tags are created there or while writing. Settings is kept for security (passkeys, password, the
connected AIs and tokens).

**A row is the node itself.** Wherever a node appears with its bullet (a
journal day, a page, the agenda, a tag page, a subject's sections,
search results, a page title) it is edited in place, with its children, and
reads exactly as it is written: references and types are atoms, rendered
the same while reading and while editing (ProseMirror), and a click places
the caret where it lands. Outline keys: Enter splits, Tab and Shift+Tab
indent and outdent, Backspace deletes an empty row or merges into the row
above, arrows move between rows, Alt+Shift+arrows reorder, Cmd+Enter
toggles a to-do. A row is saved shortly after each pause in typing, so the
same node is current in every list that shows it within a moment, and a
save refreshes every view, which keeps its folds. Leaving an outline
(another page through the palette or a link) saves what was being written
and turns a new line with text into a row, as Enter would. In lists of unrelated
nodes (a node's lists, a tag page, search results) the listed rows
stay in place: Enter saves and leaves a row rather than adding one.
Tags and references are links to their pages, while reading and while
editing.

**A selection that leaves its row selects rows.** Shift+↓ or Shift+↑ past
the edge of the row being edited, or a mouse drag from one row into
another, selects whole rows, highlighted as blocks, children included.
Shift+arrows grow or shrink the selection, ⌘C copies it as an indented
text outline, ⌘X or Backspace moves it to the trash, Escape or a click
elsewhere ends it, an arrow or Enter edits its first or last row again.

The agenda and the week show only nodes with a date; a meeting noted in
the journal without one is not scheduled.

Preferences live on the server, so they follow the owner across devices,
and are chosen from the search (⌘K, "theme", "size", "outlines"): how
outlines open (expanded or collapsed), the theme (system, light, dark) and
the interface size (small to larger, so the browser's zoom is never
needed). Each outline can also be expanded or collapsed at once.

## 6. MCP

Streamable HTTP at `/mcp`. Server instructions explain Vrac's conventions to
any client: write in today's journal by default, search before creating,
reference by ID, use the user's tags as listed, never create implicitly.

Tools (V1):

| Tool | Purpose |
|---|---|
| `vrac_today` | date, time zone, today's journal, what is dated today, overdue and open to-dos with their subjects, the tags |
| `vrac_search` | the query of section 4 |
| `vrac_resolve` | names to candidate nodes, aliases included |
| `vrac_get` | a node, its bounded children and, by tag, the counts of what concerns it |
| `vrac_day` | a journal day |
| `vrac_types` | the user's tags, with the to-do trait |
| `vrac_write` | create node trees, by default in today's journal, with checkbox and date |
| `vrac_update` | text, tags, checkbox, date or aliases, with the expected revision |
| `vrac_move` | change parent or order |
| `vrac_trash` | move to the trash, or restore |
| `vrac_create_type` | create a tag explicitly, plain or to-do |
| `vrac_activity` | recent changes with actor and source |

Results show text with `[[Name|ID]]` references.

`vrac_update` keeps a node's tags when its new text writes none, so a
rewritten sentence does not lose its type, status or date by omission;
with `keep_types: false` the node has exactly the tags its text writes,
none if it writes none.

Every mutation takes a request ID and a source (for example `outlook`); the
actor comes from the authenticated client.

### Skill

The repository ships a `vrac` skill for Claude (and equivalents where
supported). The AI uses Vrac on its own initiative: it records without asking
(after triaging mail or messages, a meeting, a decision, an analysis, or
work the user describes) and says in one line what it recorded, and it reads
the context of any project, client or person the user mentions before
answering. It writes by searching and resolving first, reusing existing
nodes, referencing subjects, tagging with the user's own tags, confirming
only creations with the user, and always setting the source. It replaces
the user's current Memos skill.

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
  Migration 7 turned the built-in types into the user's tags (removing the
  unused ones), gave `task` the to-do trait and moved due dates and starts
  into one date field; migration 8 dropped the checkboxes of nodes without
  a to-do tag.
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
