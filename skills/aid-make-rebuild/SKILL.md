---
name: aid-make-rebuild
description: The Rebuild stage of the Make loop, loaded by aid-make and not an entry point. Once the changes are complete and the state record is closed, it squashes the iteration history into a presentable, feature-by-feature series of commits on a new branch for whoever reviews the PR or merge, drops the working state, and proves the final tree matches.
user-invocable: false
---

# Make, Rebuild: a history worth reading

**Loop:** make

Building leaves a trail of small commits, review fixes, and working-state updates. The rebuild squashes that iteration and noise into a presentable history, organised feature by feature, for whoever reviews the PR or merge. **Readability is the main goal**; bisectability isn't the primary use case.

## Before you start

- Review is clean (aid-make-review).
- Every state item is resolved (aid-learn-history) and the tool's finish check passes (`aid finish`, aid-graph).

## Rules

- **Never rewrite the original branch.** Create `<branch>-rebuild` (or a name the user picks) from the merge base. The original stays as the reference until the user deletes it.
- **One commit per feature,** in an order a reader can follow. Each commit includes its own tests and the PRD sections it delivers, and its message describes the feature, not the iteration.
- **Buildable where practical.** Commits that build on their own are preferred, but that isn't always possible, especially on long projects. Say which ones don't.
- **The working state is dropped.** `.aid/` isn't carried into the rebuild. PRDs are, because they're product documentation.
- **The end state must match exactly.** The final tree of the rebuild branch equals the original branch's final tree, minus `.aid/`. Check it with a diff and show the output.
- **Linear history.** Rebase and fast-forward. No merge commits.

## Procedure

1. Confirm the feature list and order with the user: the commit subjects, and which paths go in each.
2. Create the rebuild branch from the merge base.
3. For each feature, check out its paths from the original tip (`git checkout <orig> -- <paths>`), or hand-split mixed files, then commit.
4. Diff the rebuild tip against the original tip, excluding `.aid/`, and show the result. It must be empty.
5. Hand back to the user. Pushing, replacing the original branch, and deleting branches are the user's calls.

When one file mixes two features' changes, either split the hunks by hand or merge the two features into one commit. Ask the user which they prefer, and say what the split costs.
