# AI evaluation

`go run ./scripts/ai-eval [-model haiku] [-only name]` measures how well an
AI uses Vrac through MCP. Each scenario starts from a fresh sample workspace
(`scripts/seed`), starts a server, and runs one non-interactive Claude Code
session (`claude -p`) connected only to that Vrac, with the `vrac` skill and
no other tool. The AI writes under a token named `eval`, so the activity
feed tells exactly what it created or changed; checks then read the
database and the answer. Results go to `ai-eval.json`.

The scenarios use the local `claude` CLI and its account: one session each.

## Scenarios

| scenario | the AI must |
|---|---|
| read-open-tasks | list the open tasks on Northwind, including its sub-project, and write nothing |
| read-decisions | list the decisions on Globex |
| read-next-meeting | give the next meeting about Northwind and its time (computed at run time) |
| read-overdue | list the overdue tasks |
| read-why | explain a decision from the notes around it |
| write-mail-triage | from an email, set the due date of the existing task and move the existing meeting, without duplicates or new entities |
| write-meeting-notes | record one meeting with Alice about Globex, its decision, a task due next Wednesday, and move the related event by a week |
| write-task-done | mark the right task done, creating nothing |
| write-decision-evolves | update the existing decision in place |
| write-new-prospect | create the new person and company once, and the call tomorrow at 11:00 |

The scenarios were written for version 1's built-in types; since the model
became tags without fields (2026-11-07), the sample workspace creates the
same tags itself and dates are one generic field, so the checks still apply
but the results below predate that change.

## Results (2026-11-05)

| model | runs | passed | tool calls | time | cost (API equivalent) |
|---|---|---|---|---|---|
| CLI default (Opus) | 3 | 30/30 | 4.4 per scenario | 13 s | $0.09 per scenario |
| Haiku | 2 | 17/20 | 4.4 per scenario | 11 s | $0.003 per scenario |

Findings that changed Vrac:

- A first mail-triage scenario said "before Thursday" on a Thursday:
  ambiguous for both models. Prompts now name a day three days ahead.
- Haiku resolved weekdays less reliably. `vrac_today` now returns the
  weekday and the dates of the next seven days.
- Haiku merged "update the import documentation" into the existing "update
  the import schema" task: the skill pushed deduplication too hard. It now
  says that similar wording is not the same action; Haiku then passed this
  scenario in 2 of 3 runs, against 0 of 2 before.

Both models asked for confirmation where the input was ambiguous, wrote
nothing for questions, and set a source on every change.
