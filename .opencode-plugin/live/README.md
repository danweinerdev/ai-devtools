# OpenCode 2 live suite

`make test-opencode-e2e` proves, against a real OpenCode 2 server and real models, that the plugin does what the offline tests assume. It makes real model calls and can incur provider costs, so it runs **locally only**: never in `make check` or CI.

## What it checks

| Row | Passes when |
|---|---|
| `registration` | exactly one `ai-development` plugin is active and it was loaded from this checkout's directory (otherwise `PLUGIN_NOT_FROM_CHECKOUT`), and it lists all seven `aid-*` agents with the right modes, and every `aid-*` skill |
| each mapped `aid-*` subagent | its registered model, its child session and every assistant message in it agree on the model its `aid.json` profile names, through one native subagent call with no model override, dispatched from a parent session running on a different model from the config (printed as `parent=`) |
| `aid-orchestrator` (if mapped) | its own session, created with no model, runs its turn on the orchestrator's profile model, variant included |
| `guard` | a dispatch carrying a planted secret is denied by `aid guard`: the subagent call errors and no child session exists |

Evidence must agree across every source; missing evidence is **UNVERIFIED**, never a pass. A variant the catalog doesn't offer is expected to be stripped. Agents may share a model. A row is UNVERIFIED (`MODEL_NOT_DISTINCT`) only when its model, variant included, is the one its parent session ran on: an unrouted subagent would inherit that model, so routing can't be told apart from inheriting. The suite avoids that by giving each worker's parent session another model from your profiles (a different model first, else a different variant); with only one model in the config it can't, and says so: add a profile with a different model. Unmapped agents run on the session's model and are listed as skipped.

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
- `--agents all|ID,...`: rows to run (`aid-orchestrator`, any `aid-*` subagent, `guard`).
- `--keep`: keep the scratch project even on success (failed and unverified runs keep it automatically).
- `--timeout SECONDS` (or `AID_E2E_TIMEOUT`): each row's deadline, default 300. A row past it is UNVERIFIED (`TIMEOUT`): its session is interrupted, the last server-log lines for its sessions are printed, and the suite moves on to the next row.

Rows run one at a time and each prints its result as soon as it finishes (`running <row>…`, then the result with where its time went, e.g. `(12.2s: parent 4.1s, child 6.0s, other 2.1s)`: parent is the parent session's model turns minus the subagent call, child the child session's model turns, other everything else, from the messages' `time.created`/`time.completed` and the subagent call's `time.ran`); a `SUMMARY` and the `RESULT` line follow at the end. The server's log is written, with its password redacted, to `opencode-server.log` in the scratch project, which is kept unless every row passes.

The suite's server is isolated so it can only test this checkout: the scratch project lists the repository directory as its plugin (OpenCode 2 refuses a path to `index.js`), and the server runs with `XDG_CONFIG_HOME` pointed at a scratch copy of your global `opencode.json(c)` with its `plugins` removed (providers and other settings kept; `OPENCODE_CONFIG`/`OPENCODE_CONFIG_DIR` dropped). Credentials stay in OpenCode's data directory, untouched. The copy is 0600 and deleted at the end, even when the scratch project is kept.
- `--opencode PATH`: the OpenCode executable.

Without a config, OpenCode 2, aid or git, it prints `SKIPPED: <reason> (<fix>)` and exits 2 without starting anything.

Exit codes: **0** all pass, **1** any failure, **2** unverified, skipped, could not run, or interrupted.

## Cleanup

The suite starts a private loopback `opencode serve` on the normal OpenCode database (provider connections live there). Afterwards it interrupts and deletes its probe sessions and their children, even with `--keep`, and stops only its own server. Server output is never echoed (it carries the server password). Ctrl-C prints `INTERRUPTED`, does the same cleanup, and exits 2; a second signal during cleanup kills the server and exits.

The offline half (row planning, scratch config, expectations, the guard verdict, the evidence comparison) runs in `make test-opencode` with no model calls.

Ported from opencode-model-router's live suite, rebuilt for OpenCode 2 and the aid-* roster.
