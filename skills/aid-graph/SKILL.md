---
name: aid-graph
description: How to drive the task graph of the aid tool: the tasks.json format, every command with its flags, the --json output, and the usage patterns for building a graph from a breakdown, claiming and completing tasks, splitting and dropping tasks when the plan changes, and re-running every task's checks as one batch. Use it whenever Make work creates, reads, or changes its task graph.
user-invocable: false
---

# The task graph (`aid`)

**Loop:** all

The task graph is the working state Make runs from: `.aid/<name>/tasks.json`, held by the `aid` command-line tool. Never edit the file by hand. Every command validates the whole graph on load, and every write is locked and atomic, so parallel callers are safe. `aid` never calls git and stores no commit IDs, so rebasing or cherry-picking work back from a worktree invalidates nothing.

Each command acts on one piece of work, found by walking up from the current directory to the nearest `.aid/`; the directory holding it is the repository root. `--work <name>` selects the work and can be left out when only one exists. `--json` gives machine-readable output.

## The file

```json
{
  "version": 1,
  "tasks": [
    {
      "id": "store",
      "title": "The store (PRD › Make)",
      "needs": [],
      "footprint": ["internal/store/"],
      "done": {"result": "round-trip and rejection tests pass", "checks": ["go test ./internal/store"]},
      "state": {"status": "in-progress", "claim": {"holder": "aid-oracle", "expires": "2026-01-02T05:04:05Z"}}
    }
  ]
}
```

What you author sits at the top of each task; what the tool owns sits inside `state`, so an edit to one can never clobber the other.

| Field | Meaning |
|---|---|
| `id` | unique; a slug or a lowercase UUID (`^[a-z0-9][a-z0-9-]*$`) |
| `title` | what it builds, citing the PRD section it serves |
| `needs` | IDs of the tasks it depends on |
| `footprint` | clean relative paths, files or directories, that it creates or edits |
| `done.result` | the measurable result |
| `done.checks` | commands that prove it: non-interactive, run from the repository root, pass or fail by exit code |
| `state.status` | `todo`, `in-progress`, `done`, `blocked`, or `dropped` |
| `state.claim` | `in-progress` only: `holder` and lease `expires` |
| `state.reason` | `blocked` and `dropped` only |

Validation on every load rejects, all at once: unknown or missing keys (with a did-you-mean), nulls, duplicate IDs, needs that don't exist, a task needing itself or a need twice, cycles (each knot once, as `cycle: a → b → a`, naming any other tasks caught in it), a claim on anything but `in-progress`, a reason on anything but `blocked` or `dropped`, a `todo` or `blocked` task needing a dropped one, an `in-progress` task needing one that isn't `done`, and a `done` task needing one that isn't `done` or `dropped`. So no edit can leave a held task waiting on unfinished work: `update-task`, `split-task` and `drop-task --replaced-by` refuse such a rewiring; release the task first.

A task's **effective** status is its status, except that an `in-progress` task whose lease has lapsed counts as `todo`. The **ready frontier** is every effectively `todo` task whose needs are all `done`.

## Commands

| Command | Does |
|---|---|
| `init <name>` | creates `.aid/<name>/` with an empty graph and state record, atomically; refused if it exists |
| `add-task --title --footprint --result --check [--id] [--needs]` | adds a `todo` task. Its needs must already exist, so add in dependency order. Without `--id` a UUIDv7 is generated; a duplicate ID is refused |
| `update-task <id> [--title] [--needs] [--footprint] [--result] [--check]` | changes only the flags given; a list flag replaces the whole list (`--needs=` clears it). Never changes status; a dropped task can't be edited, and neither can a task under a live claim, its holder included |
| `split-task <id> --into <ids>` | the parts are added first with `add-task`. They inherit the task's needs, every task that needed it now needs all the parts, and it becomes `dropped` with the reason `split into …`, in one write. Refused on a `done` or `dropped` task, a live claim, or a part the task itself needs (directly or through a chain) |
| `claim-task <id> [--holder <who>] [--lease 2h]` | marks a ready task `in-progress` for the holder until the lease lapses. One holder at a time; the same holder claiming again renews the lease |
| `release-task <id> [--holder <who>]` | an `in-progress` task back to `todo`, claim cleared; only its holder may release a live claim. On a `blocked` task it unblocks it back to `todo` |
| `block-task <id> --reason <text> [--holder <who>]` | a `todo` or `in-progress` task to `blocked`, claim cleared; a live claim can only be blocked by its holder |
| `complete-task <id> [--holder <who>]` | runs the task's checks without holding the lock, then marks it `done` only if every check passed and it's still held by the holder with the same checks. A failure leaves it `in-progress` and shows the failing output |
| `drop-task <id> --reason <text> [--replaced-by <ids> \| --cascade]` | marks it `dropped`; see below |
| `check [--paths <file>…] [--strict]` | runs the checks of every task that isn't dropped, or only those whose footprint covers a given path; pass or fail per task, exit 1 if any fails. Paths no task covers are listed but pass, since unowned files (often docs) need no re-proving; `--strict` makes them fail. Writes nothing |
| `ready` | lists the frontier |
| `show [ids…]` | no IDs: every task in file order with its effective status and needs, then a summary. With IDs: those tasks in full, including the tasks that need them |
| `validate` | checks both files and the links between them, reporting every problem; writes nothing |
| `finish` | succeeds only when the graph is non-empty, every task is `done` or `dropped`, no claim is live, and no state item is open (aid-state); checked inside the locked write |

List flags (`--needs`, `--footprint`, `--into`, `--replaced-by`, `--paths`) take comma-separated values or repeat. `--check` repeats, one command per flag, because commands contain commas.

**Footprint matching** (`check --paths`): an entry covers a path when they're equal or the path is under it, comparing whole path segments, with or without a trailing slash. `cmd/aid` covers `cmd/aid/main.go`; `cmd/ai` doesn't. Paths may be absolute or relative to the current directory; one outside the repository root is refused.

## Output

Text output is for people. With `--json`:

- task commands print the task with its `effective` status (`show <ids>` adds `dependents`); `ready` and `split-task` print lists of tasks; `show` prints `{"tasks", "items": {"open", "resolved"}}`;
- `complete-task` prints `{"id", "done", "checks"}` and `check` prints `{"tasks": [{"id", "status", "passed", "checks"}], "passed", "failed"}`, each check being `{"command", "passed", "exitCode", "seconds"}` plus `timedOut`, `error` and `output` when relevant;
- `drop-task` prints `{"task", "dropped", "rewired", "keptAsHistory", "dependents"}`, and its refusal's `detail` is `{"id", "dependents": [{"id", "status", "blocks"}], "transitive"}`; `finish` prints `{"done", "dropped"}`.

Every refusal exits 1, writes the message to stderr, and with `--json` also prints `{"error", "detail"}` to stdout. `detail` carries what an agent needs to act on it: the failing checks, the dependents blocking a drop, the tasks and items keeping `finish` from succeeding, or the problems `validate` found.

## Patterns

**Building the graph from a breakdown.** After the user approves the breakdown text (aid-make-breakdown), run `init <name>`, then one `add-task` per step, in dependency order so every need already exists. Then `validate` and `ready` to show the starting frontier. From here the graph is authoritative; the breakdown text isn't updated.

**Writing good checks.** A check is the proof a task is done, and it stays attached to the task for the whole feature. Prefer the specific tests written for the task (one test file, one test name) over the whole suite, so the batch stays meaningful and fast. Never an interactive command, and never one that depends on the current branch or commit. Each check gets ten minutes; a timeout or an interrupt kills everything it started. Checks run through the shell with the caller's environment, so `tasks.json` is code: review changes to it as you would a script. The lock is advisory and may not hold on a network filesystem, so keep `.aid/` on local disk.

**Writing footprints.** Paths relative to the repository root. They survive rebases and drive `check --paths`, so keep them accurate when a task's scope changes (`update-task`).

**The loop.** `ready` → `claim-task` → build → `complete-task`. `--holder` defaults to `$AID_HOLDER`, else `main`, so an agent working alone never passes it; a delegated worker passes its role (`--holder aid-mechanic`) on every status command, which keeps one holder per task. If the checks fail, the task stays `in-progress`; fix and complete again. If the work can't go on, `block-task` with the reason (`release-task` resumes it later), or `release-task` to hand it back. A long build renews its lease by claiming again.

**When the plan changes.** No plan survives implementation. Change the graph, not the breakdown:
- a task turns out bigger than planned: add the parts, then `split-task`;
- its scope, needs, or checks were wrong: `update-task` (a held task is released first, so its definition of done never changes under its holder);
- it no longer applies: `drop-task`.

**Dropping a task.** `drop-task` is refused while a claim is live, and while any `todo`, `in-progress` or `blocked` task still needs it. The refusal lists only the direct dependents, the ones whose needs must move, each with its status and whether it blocks, plus a count of the tasks with work ahead that need them in turn.

When that count is more than zero, handle the whole subtree rather than task by task:
- **reassign all:** `--replaced-by <ids>` moves every blocking dependent's need onto the replacements in the same write;
- **drop all:** `--cascade` drops the task and every `todo`, `in-progress` or `blocked` task below it, in one write with the same reason. It stops at `done` tasks, and it's refused, changing nothing, if any task in the subtree holds a live claim.

When it's zero, decide each blocking dependent, then drop again:
- it still applies and the dropped task was replaced: `update-task` its needs, or `--replaced-by <ids>`;
- it still applies and the prerequisite went away: `update-task` to remove the need;
- it no longer applies: `--cascade`, or drop it first.

Dependents that are already `done` don't block and aren't reopened: they keep their need on the dropped task as a record of what they were built on, and their checks stay in the batch to show whether the drop broke them. Tell the user about every drop and how its dependents were resolved; the output lists what was dropped, the rewired dependents, and the ones kept as history.

**The batch.** `check` re-runs every task's checks together. Review runs it at the completion gates (aid-make-review), with `--paths` to limit it to what a change touched. Nothing tracks staleness; when in doubt, run the batch again.
