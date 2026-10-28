---
name: aid-state
description: How to drive the state record of the aid tool: the notes, follow-ups, deferred findings, and questions that aren't tasks in the graph but must be followed to the end of a piece of Make work. Covers the state.json format, the item kinds, the commands and their flags, when to record an item, how each is resolved to a durable home, and the finish check. Use it whenever something is noticed during Make that isn't a task, and when closing the work out.
user-invocable: false
---

# The state record (`aid`)

**Loop:** all

The state record is `.aid/<name>/state.json`, held by the `aid` command-line tool alongside the task graph (aid-graph). It tracks everything that isn't a task in the chain but must be followed to the end. Never edit the file by hand: every command validates it on load, and every write is locked and atomic. `--work <name>` and `--json` work as they do for the graph.

The rebuild drops `.aid/`, so every item must be resolved before then. That's what keeps follow-ups from floating.

## The file

```json
{
  "version": 1,
  "items": [
    {"id": "i1", "kind": "note", "status": "resolved", "text": "the lock is per work, not per file",
     "resolution": {"as": "task", "where": "store-lock"}},
    {"id": "i2", "kind": "follow-up", "status": "open", "text": "port the guards into aid"}
  ]
}
```

| Field | Meaning |
|---|---|
| `id` | `i1`, `i2`, …, assigned in sequence; never reused, even after an item is resolved |
| `kind` | `note`, `follow-up`, `finding`, or `question` |
| `status` | `open` or `resolved` |
| `text` | what was noticed, with its evidence |
| `resolution` | resolved items only: `as` (`done`, `task`, `prd`, or `guidance`) and `where` it went |

Validation rejects unknown or missing keys, duplicate IDs, an unknown kind or status, empty text, an open item with a resolution, a resolved one without, and an item resolved `as` a task that doesn't exist in the graph (`aid validate`).

## Commands

| Command | Does |
|---|---|
| `add-item --kind <kind> --text <text>` | records a new open item and prints its ID |
| `update-item <id> [--kind] [--text]` | changes only the flags given; resolved items can still be edited |
| `resolve-item <id> --as done\|task\|prd\|guidance --where <text>` | closes an open item, recording where it went. With `--as task`, `--where` must be an existing task ID. Resolving twice is refused |
| `items [--kind <kind>] [--status open\|resolved]` | lists items; `--json` prints them |

## When to record

Record straight away, not at the end. Anything noticed that isn't part of the current task:

- **note:** context worth keeping until the work finishes (a surprising behaviour, a constraint discovered);
- **follow-up:** something that should happen, but not in this task;
- **finding:** a review finding deferred rather than fixed now (aid-make-review);
- **question:** something only the user can answer. If it blocks work, ask now (aid-think-clarify); if not, it can wait for the close-out.

Specialists put these in their reports; the orchestrator records them.

## Resolving

Each item ends in exactly one durable home (aid-learn-future-work):

- **done:** handled; `--where` says where;
- **task:** this feature still needs it; add it to the graph first, then resolve with its ID;
- **prd:** a behaviour or layout change, or an open question, proposed as a fragment (aid-prd R1);
- **guidance:** a process or tooling lesson, proposed as an edit to the project's agent guidance or a skill.

Resolution normally happens in aid-learn-history, one item at a time with the user. `aid finish` refuses while any item is open, listing each one; with `--json` its refusal detail carries `{"tasks", "items"}`.
