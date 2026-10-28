---
name: aid-think-explore
description: The Explore stage of the Think loop, loaded by aid-think and not an entry point. Gathers enough evidence-backed facts for an informed conversation: the PRDs in scope, the sources the user names, and the code behind the PRD's code paths, with wide sweeps delegated to aid-explorer. Produces a fact sheet of claims with their evidence.
user-invocable: false
---

# Think, Explore: gather the facts

**Loop:** think

Goal: enough facts to have an informed conversation, not every fact there is. aid-think's ground rules apply: evidence on every claim, ranked sources, search trails for absence.

- Read the PRD(s) in scope. Scan `docs/PRDs/` frontmatter to find related ones.
- Read the sources the user names: branches (`git log`, `git diff main...<branch> --stat`), plans, reference folders, older PRDs. Note what each one claims and how fresh it is.
- Read the code the PRD's `code:` paths point at, enough to check claims and not more.
- **Delegate wide sweeps.** When answering means reading many files or several branches, dispatch aid-explorer with a narrow question per sweep, and run independent sweeps in parallel. Keep the conclusions and drop the file dumps. Without subagents, do the sweeps yourself one at a time.

## Output

A short **fact sheet**. Each line is a claim plus its evidence, grouped by topic. Mark stale or superseded sources as such. Superseded work ("the downloader is no longer a later phase") is the most common source of conflict.

Hand the fact sheet to aid-think-challenge.
