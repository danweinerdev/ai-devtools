# OpenCode 2 live suite

`make test-opencode-e2e` proves, against a real OpenCode 2 server and real models, that the plugin does what the offline tests assume. It makes real model calls and can incur provider costs, so it runs **locally only**: never in `make check` or CI.

## What it checks

| Row | Passes when |
|---|---|
| `registration` | the plugin is active and lists all seven `aid-*` agents with the right modes, and every `aid-*` skill |
| each mapped `aid-*` subagent | its registered model, its child session and every assistant message in it agree on the model its `aid.json` profile names, through one native subagent call with no model override |
| `aid-orchestrator` (if mapped) | a worker's parent session ran on the orchestrator's profile model (the default model) |
| `guard` | a dispatch carrying a planted secret is denied by `aid guard`: the subagent call errors and no child session exists |

Evidence must agree across every source; missing evidence is **UNVERIFIED**, never a pass. A variant the catalog doesn't offer is expected to be stripped. Rows whose agents share one model can't be told apart and are UNVERIFIED (`MODEL_NOT_DISTINCT`): give each mapped agent its own model for a full pass. Unmapped agents run on the session's model and are listed as skipped.

## Running it

Requirements:

- OpenCode 2 on PATH (or `--opencode PATH`), with provider access to every model your profiles name. Provider credentials stay where OpenCode keeps them: its own login store, or whatever environment variables your OpenCode config references (for example a gateway API key). The suite never reads, writes or prints credentials.
- `aid` on PATH (`make install-aid`), node 20+, and git.
- An `aid.json` with profiles for the agents you want proved. It's never modified: the suite copies it into a scratch project, adds a sentinel agent (routed like the first mapped subagent, to prove the scratch config is the one loaded) and a sensitivity rule for the guard row.

```sh
make test-opencode-e2e    # the aid.json the plugin uses: .opencode/aid.json here or above, else ~/.config/opencode/aid.json
AID_E2E_CONFIG=/path/to/aid.json make test-opencode-e2e
AID_E2E_CONFIG=/path/to/aid.json make test-opencode-e2e E2E_ARGS="--agents aid-oracle,guard --keep"
node .opencode-plugin/live/e2e.js --config /path/to/aid.json --opencode /path/to/opencode
```

- `--config PATH` (or `AID_E2E_CONFIG`): the aid.json to prove. Without either, the suite proves the one the plugin would use, as `aid config --harness opencode` resolves it.
- `--agents all|ID,...`: rows to run (`aid-orchestrator`, any `aid-*` subagent, `guard`). Include a subagent with `aid-orchestrator`: its evidence comes from a worker's parent session.
- `--keep`: keep the scratch project even on success (failed and unverified runs keep it automatically).
- `--opencode PATH`: the OpenCode executable.

Without a config, OpenCode 2, aid or git, it prints `SKIPPED: <reason> (<fix>)` and exits 2 without starting anything.

Exit codes: **0** all pass, **1** any failure, **2** unverified, skipped, could not run, or interrupted.

## Cleanup

The suite starts a private loopback `opencode serve` on the normal OpenCode database (provider connections live there). Afterwards it interrupts and deletes its probe sessions and their children, even with `--keep`, and stops only its own server. Server output is never echoed (it carries the server password). Ctrl-C prints `INTERRUPTED`, does the same cleanup, and exits 2; a second signal during cleanup kills the server and exits.

The offline half (row planning, scratch config, expectations, the guard verdict, the evidence comparison) runs in `make test-opencode` with no model calls.

Ported from opencode-model-router's live suite, rebuilt for OpenCode 2 and the aid-* roster.
