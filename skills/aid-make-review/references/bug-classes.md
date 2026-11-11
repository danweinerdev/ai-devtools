# Bug classes for the quality lens

The quality lens works from the diff and the code around it, with **no PRD, brief, plan, or task graph**. Nothing in this checklist needs that context: judge the code on its own behaviour, and don't ask for intent that wasn't in the inputs.

## Severity

Grade by impact, not by confidence or how hard the bug was to find.

| Severity | Impact |
|---|---|
| critical | Silent data corruption, or a wrong security decision (an auth or permission call that lets the wrong actor through). |
| high | A user-visible failure on a normal path. |
| medium | A real defect, but on an uncommon path or with a workaround. |
| low | Unreachable in practice, or cosmetic. |

## The eight classes

### Logic

- **Looks for:** inverted conditions, wrong comparison or boolean operators, off-by-one errors, contradictory or dead branches, iterating the wrong collection, state assumed to be set elsewhere, mutable default values, type confusion.
- **Leaves to:** edge cases for null, empty, and boundary inputs; error handling for failure paths that are swallowed rather than wrong; concurrency for behaviour that only breaks under parallel access.

### Edge cases

- **Looks for:** null or missing values dereferenced, empty collections indexed, zero divisors, negative inputs, overflow, unicode and byte-versus-codepoint mistakes, whitespace-only strings, date and timezone boundaries, path separators and `..`.
- **Leaves to:** logic for wrong operators that fail on ordinary inputs; security when the boundary value is the mechanism of an exploit; interface contracts when the bad value crosses a public boundary.

### Error handling

- **Looks for:** swallowed exceptions, failures normalised into success, a specific failure mapped to the wrong error code, lost exception causes, unchecked error returns, retries with no backoff or cap, cleanup that masks the original error, error paths that leak.
- **Leaves to:** resources for handles, connections, and locks not released on the error path; concurrency for retry storms and races between failures; interface contracts for wrong status codes or response shapes.

### Concurrency

- **Looks for:** missing locks or awaits, check-then-act races, shared mutable state across threads or requests, locks held across await points, inconsistent lock order, unbounded parallelism, unretained tasks and swallowed task exceptions, blocking calls inside async work.
- **Leaves to:** logic for single-threaded correctness; resources for unbounded pools, queues, and memory growth; error handling for retry and backoff policy.

### Resources

- **Looks for:** files, sockets, database connections, locks, or timers not released on every path; unbounded collections, caches, or queues; memory retained longer than needed; missing `defer`/`finally`/context cleanup; work started without a stop path.
- **Leaves to:** error handling for an error swallowed while cleaning up; concurrency for races over shared resource state; logic for off-by-one sizes and allocations that are wrong before they are large.

### Security

- **Looks for:** injection into SQL, commands, templates, paths, or HTML; missing authentication or authorization checks; ownership not verified; secrets or PII in code, logs, errors, or URLs; weak or misused cryptography; timing-sensitive comparisons; permissive CORS or missing CSRF protection; unsafe deserialization.
- **Leaves to:** logic for correctness that isn't security-relevant; edge cases when the bad value is only one input rather than the exploit; interface contracts for shape drift without a security consequence.

### Interface contracts

- **Looks for:** response or request shape drift, missing required fields, undocumented fields leaking internals, wrong status codes or error codes, content-type mismatches, method semantics (GET that mutates, PUT acting as PATCH), pagination and auth-header inconsistency across endpoints, public signatures that no longer match their callers.
- **Leaves to:** logic for internal behaviour behind a stable interface; error handling for the failure path inside a correctly shaped response; security for auth and data-exposure flaws in the contract.

### Test quality

- **Looks for:** tests that cannot fail or prove nothing — no assertion; a fixed sleep instead of waiting on a condition; a skip or expected-failure marker; logic in the test body; more mocks or verifications than assertions; a mock asserted against its own configured return; presence-only assertions where a value is available. It also hunts **weakened proof**: a test deleted, skipped, or loosened; an assertion removed or rewritten to match the output; a check narrowed until it no longer reaches the code it names; a focused test (`.only`, `fit`, `fdescribe`) that silently narrows the suite.
- **Leaves to:** logic, edge cases, error handling, concurrency, resources, security, and interface contracts for defects in the production code itself. This class judges whether the test proves its behaviour, not whether the behaviour is right.

Common skip and expected-failure markers: `it.skip`, `test.skip`, `describe.skip`, `xit(`, `xdescribe`, `@Disabled`, `pytest.mark.skip`, `xfail`, `#[ignore]`, `t.Skip`, `@unittest.skip`, `@Ignore` (JUnit 4), `[Ignore]` (NUnit/MSTest), `[Fact(Skip = …)]` (xUnit). Focus markers that silently narrow a suite are weakened proof too: `.only`, `fit`, `fdescribe`.

## Evidence rules

**Quote verbatim.** Every finding quotes the code it is about, and the quote must appear verbatim in the cited lines. A finding whose quote doesn't appear there is dropped as unfounded. Cite the smallest span that shows the problem, with the file and line numbers.

**Dedupe.** Findings on the same file, with overlapping cited lines, and the same class are one finding. Merge them, keep the strongest evidence, and list every affected line. Different classes stay separate even when they overlap.
