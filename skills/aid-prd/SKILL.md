---
name: aid-prd
description: The PRD contract. A PRD is a living, user-owned document describing a feature as if it were finished now. Use it whenever you create, read, update, split, or check a PRD; when code and a PRD disagree; when a question of scope or behaviour comes up (the PRD is the first reference); or before proposing any edit to a document under docs/PRDs.
user-invocable: false
---

# PRD: the living feature document

**Loop:** all. Think decides what goes in a PRD, Make finds where reality differs, Learn curates it.

"PRD" is the working name for a document that describes a whole feature set. It is structured prose: description, diagrams, behaviours, interfaces. It is not a requirements checklist or a ticket.

## What a PRD is

1. **The final picture.** A PRD describes the feature as if it were done right now, even when the code is incomplete or doesn't exist yet. It answers "how does this work?" in one place.
2. **Not a decision log.** The body states what *is*, not how the team got there. History can go in an optional `## Appendix: History`, never in the body. When a decision changes something, rewrite the affected section so it reads as current truth.
3. **The first reference.** Questions about scope, expanding a component's responsibility, or how something should behave go to the PRD first, before the code or chat history.
4. **Living.** Implementation forces change, and the change gets recorded. A PRD that disagrees with the code is a defect in one of the two, and someone has to resolve it.
5. **User-owned, AI-maintained.** The user owns the content and makes the decisions. The AI drafts, keeps it current, and proposes every change. It never edits silently.

## The rules

### R1. Propose before any edit

Never write to a PRD without approval. Show a **change fragment** for each edit, then wait for **approve**, **deny**, or **edit**:

```markdown
### PRD change — <PRD file> § <section heading>
**Why:** <trigger: inconsistency found / user decision / implementation reality>
**Evidence:** <file:line, command output, or the user's words>

**Current**
> <exact current text, or "(new section)">

**Proposed**
> <replacement text>

**Ripple:** <other sections or PRDs this touches, or "none">
```

- Present fragments **one at a time**. The next waits until the user has approved, denied, or edited the current one. When several changes are tied together, say so, and still present them in order.
- Apply exactly what was approved. If the user edits a fragment, apply their version and don't re-polish it.
- A denied fragment is an answer. Don't re-propose it without new evidence.
- Frontmatter-only bookkeeping (`updated:` date, adding a `related:` link) can go with an approved fragment. It never justifies an edit on its own.

### R2. Whoever finds an inconsistency proposes the fix

The agent that discovers a mismatch between a PRD and the code, or between two PRDs, writes the fragment. Don't leave it as a TODO for someone else.

- **Main conversation:** propose it to the user directly.
- **Subagent:** subagents can't talk to the user. Put the fragment in your final report under `## PRD changes proposed`, and the orchestrator presents it unchanged.

The PRD is the intent and the code is the reality. Assume neither is right. Every fragment that comes from a mismatch must say which way it resolves:
- **PRD follows code:** the implementation made a better or necessary choice, so record it.
- **Code follows PRD:** the code is wrong or incomplete. The PRD stays put and the gap becomes Make work.
- **Neither:** the mismatch reveals an open question. Add it to `## Open Questions` (see R3).

### R3. Unanswered questions belong to the user

If the PRD doesn't answer a question, the AI doesn't answer it either. It may:
- frame the question, list the options with trade-offs, and **recommend** one;
- add the question to the PRD's `## Open Questions` (via a fragment);
- continue with work that doesn't depend on the answer.

It may not build the provisional answer into code or prose. "Pending confirmation" is not an answer.

### R4. Split before it sprawls

Document sprawl is inevitable. Manage it by carving PRDs up, not by letting them grow forever. Propose a split when:
- one PRD covers components that change for different reasons, or that someone could own separately;
- someone can't scan a section without scrolling through unrelated material;
- a sub-feature has its own open questions, lifecycle, or testing story.

A split is a set of fragments: the new PRD's full draft, the parent section shrunk to a short summary plus a link, and the frontmatter updates (`parent`/`children`). The user approves the split like any other change.

## Location and naming

- Default home: `docs/PRDs/`. A project can say otherwise in its `AGENTS.md`/`CLAUDE.md`.
- One file per feature: `<FeatureName>-PRD.md` in PascalCase, e.g. `ArkDesktopApp-PRD.md`. A child PRD from a split carries its parent's prefix: `ArkDesktopApp-Updater-PRD.md`.
- To find PRDs, scan frontmatter (`title`, `summary`, `tags`, `code`) before reading bodies. Frontmatter exists to make that scan cheap.

## Frontmatter

```yaml
---
title: Ark Desktop App
summary: One or two lines. What this feature is and who it serves.
status: draft          # draft | active | shipped | split | retired
tags: [desktop, ui, rust]
code:                  # paths this PRD governs; how an agent finds the PRD for a file
  - apps/ark-desktop
  - crates/ark-desktop-core
parent: null           # file name of the parent PRD, if this one was split out
children: []           # file names of PRDs split from this one
related: []            # file names of PRDs this one depends on or is depended on by
updated: 2026-10-27
---
```

- `status` covers the feature's life, not the document's quality. `split` means the content now lives in `children`, and the file is kept as an index.
- Paths are repository-relative. Never put credentials, hostnames, usernames, or machine-specific absolute paths in a PRD.

## Body skeleton

Use these headings in this order and drop the ones that don't apply. Template: [references/template.md](references/template.md).

1. `## Overview`: what it is, why it exists, what's out of scope (`### Non-Goals`).
2. `## How It Works`: the end-to-end story, with diagrams (Mermaid preferred).
3. `## Layout`: components, crates or modules, seams, and what each one owns.
4. `## Features`: grouped by major feature. Each feature lists its behaviours, and each behaviour can be tested on its own, outside the main app or UI.
5. `## Interfaces`: contracts between components and with the outside world.
6. `## Open Questions`: unanswered questions only (R3), each with options and a recommendation when there is one. Delete a question once it's answered and folded into the body.
7. `## Appendix: History` (optional): dated notes on why the picture changed. Never the source of truth.

## Checking a PRD against reality

When asked to check or curate a PRD (see aid-learn), or when a Make step touches code under a PRD's `code:` paths:

1. **Find (research, aid-explorer when it's more than a lookup):** read the PRD's relevant sections, then map each described behaviour to the code that implements it, or record the search trail where nothing does. Evidence only (file:line, plus PRD section), no verdict.
2. **Judge (the orchestrator, or aid-oracle in its own context when it has to be independent: a completion gate, the adversarial PRD challenge, or the user asking):** from that map, list each mismatch that matters. The oracle gets the map and the PRD, never a conclusion.
3. Classify it as R2 does: PRD follows code, code follows PRD, or open question.
4. Propose fragments under R1. Turn the "code follows PRD" items into Make work, not PRD edits.
