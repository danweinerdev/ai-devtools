// The live suite's offline half: planning rows from aid's config, the scratch
// config it writes, expectations from the catalog, and the guard verdict.
// None of this calls a model or starts OpenCode.
import assert from "node:assert/strict"
import test from "node:test"

import { defaultConfig, expectations, isolatedGlobalConfig, isolatedServerEnv, judgeGuard, parseArgs, planRows, pluginFromCheckout, probeFailure, probeSessionBody, rowLine, scratchConfig, serverLog, stripJsonComments, timedOut, withRowDeadline } from "../live/e2e.js"
import { compareModels } from "../live/verify-models.js"

const RESOLVED = {
  profiles: { fast: { model: "acme/fast-1" }, deep: { model: "acme/deep-2#high" }, lead: { model: "acme/lead-3" } },
  agents: { "aid-explorer": "fast", "aid-oracle": "deep", "aid-orchestrator": "lead" },
}

test("rows: mapped subagents, a mapped orchestrator, and the guard; unmapped agents are skipped", () => {
  const { rows, skipped } = planRows(RESOLVED)
  assert.deepEqual(rows.map((r) => [r.agent, r.mode]), [
    ["aid-orchestrator", "primary"], ["aid-explorer", "subagent"], ["aid-oracle", "subagent"], ["guard", "guard"],
  ])
  assert.deepEqual(skipped, ["aid-craftsman", "aid-creator", "aid-designer", "aid-mechanic"])
  assert.equal(rows.find((r) => r.agent === "aid-oracle").variant, "high")
})

test("--agents narrows the rows", () => {
  const { rows } = planRows(RESOLVED, ["aid-oracle", "guard"])
  assert.deepEqual(rows.map((r) => r.agent), ["aid-oracle", "guard"])
})

test("the scratch config adds the sentinel and the guard's rule without touching the user's object", () => {
  const user = { ...RESOLVED, sensitive: { rules: [{ name: "pii", patterns: ["x"], allow_agents: [] }] } }
  const { rows } = planRows(RESOLVED)
  const scratch = scratchConfig(user, rows)
  assert.equal(scratch.agents["aid-e2e-sentinel"], "fast")
  assert.deepEqual(scratch.sensitive.rules.map((r) => r.name), ["pii", "aid-e2e"])
  assert.equal(user.agents["aid-e2e-sentinel"], undefined)
  assert.equal(user.sensitive.rules.length, 1)
})

test("expectations strip variants the catalog lacks and drop them for the primary; shared models are allowed", () => {
  const resolved = { ...RESOLVED, agents: { ...RESOLVED.agents, "aid-mechanic": "fast" } }
  const { rows } = planRows(resolved)
  const missing = expectations(rows, [
    { providerID: "acme", id: "fast-1" },
    { providerID: "acme", id: "deep-2", variants: ["low"] },
    { providerID: "acme", id: "lead-3", variants: ["high"] },
  ])
  assert.deepEqual(missing, [])
  const by = Object.fromEntries(rows.map((r) => [r.agent, r]))
  assert.deepEqual(by["aid-oracle"].expect, { providerID: "acme", modelID: "deep-2" })
  assert.deepEqual(by["aid-orchestrator"].expect, { providerID: "acme", modelID: "lead-3" })
  // Sharing a model with another agent is fine: distinctness is judged per
  // dispatch against the parent session's model (distinctFromDefault).
  assert.equal("shared" in by["aid-explorer"], false)
})

test("expectations report routed models missing from the catalog", () => {
  const { rows } = planRows(RESOLVED)
  assert.deepEqual(expectations(rows, [{ providerID: "acme", id: "fast-1" }]), ["acme/lead-3", "acme/deep-2"])
})

test("guard verdicts: blocked passes, a child session fails, no call is unverified", () => {
  const call = (status) => ({ content: [{ type: "tool", name: "subagent", state: { status } }] })
  assert.deepEqual(judgeGuard([call("error")], []), { state: "PASS", codes: [] })
  assert.deepEqual(judgeGuard([call("completed")], [{ id: "c" }]), { state: "FAIL", codes: ["GUARD_DID_NOT_BLOCK"] })
  assert.deepEqual(judgeGuard([call("completed")], []), { state: "FAIL", codes: ["GUARD_CALL_NOT_ERRORED"] })
  assert.deepEqual(judgeGuard([{ content: [] }], []), { state: "UNVERIFIED", codes: ["SUBAGENT_CALL_MISSING"] })
  assert.deepEqual(judgeGuard([call("error")], undefined), { state: "UNVERIFIED", codes: ["CHILD_SESSION_LIST_UNAVAILABLE"] })
})

test("options: config from the flag or AID_E2E_CONFIG; bad options are refused", () => {
  assert.equal(parseArgs([], { AID_E2E_CONFIG: "/x/aid.json" }).config, "/x/aid.json")
  assert.equal(parseArgs(["--config", "/y.json"], { AID_E2E_CONFIG: "/x" }).config, "/y.json")
  assert.equal(parseArgs([], {}).config, undefined)
  assert.throws(() => parseArgs(["--agents", "nope"], {}), /Unknown row/)
  assert.throws(() => parseArgs(["--bogus"], {}), /Unknown option/)
  assert.throws(() => parseArgs(["--keep", "--keep"], {}), /Duplicate/)
})

test("default config: the aid.json aid resolves, or a clear skip", async () => {
  const fake = (result) => async () => ({ ok: true, stdout: "", stderr: "", missing: false, ...result })
  assert.deepEqual(await defaultConfig(fake({ stdout: JSON.stringify({ source: "/home/u/.config/opencode/aid.json" }) })), { path: "/home/u/.config/opencode/aid.json" })
  assert.equal((await defaultConfig(fake({ stdout: JSON.stringify({ source: null }) }))).skip, "AID_CONFIG_NOT_FOUND")
  assert.deepEqual(await defaultConfig(fake({ ok: false, stderr: "aid.json is invalid: agents.x\n" })), { skip: "AID_CONFIG_INVALID", hint: "aid.json is invalid: agents.x" })
  assert.equal((await defaultConfig(fake({ ok: false, missing: true }))).skip, "AID_NOT_ON_PATH")
  assert.equal((await defaultConfig(fake({ stdout: "not json" }))).skip, "AID_CONFIG_UNREADABLE")
})

test("options: --timeout and AID_E2E_TIMEOUT set the per-row deadline (default 300s)", () => {
  assert.equal(parseArgs([], {}).timeoutMs, 300_000)
  assert.equal(parseArgs([], { AID_E2E_TIMEOUT: "90" }).timeoutMs, 90_000)
  assert.equal(parseArgs(["--timeout", "45"], { AID_E2E_TIMEOUT: "90" }).timeoutMs, 45_000)
  for (const bad of ["0", "-5", "1.5", "abc", "86401"]) {
    assert.throws(() => parseArgs(["--timeout", bad], {}), /--timeout|Missing value/)
  }
  assert.throws(() => parseArgs([], { AID_E2E_TIMEOUT: "soon" }), /AID_E2E_TIMEOUT/)
})

test("row deadline: a stuck row is aborted as TIMEOUT and the next row still runs", async () => {
  const run = new AbortController()
  const stuck = await withRowDeadline(20, run.signal, (rowSignal) => new Promise((accept) => {
    rowSignal.addEventListener("abort", () => accept({ code: probeFailure(new Error("aborted"), rowSignal), timedOut: timedOut(rowSignal) }))
  }))
  assert.deepEqual(stuck, { code: "TIMEOUT", timedOut: true })
  assert.equal(run.signal.aborted, false)
  assert.equal(await withRowDeadline(1000, run.signal, async () => "next"), "next")
})

test("row deadline: interrupting the run aborts the row as INTERRUPTED, not TIMEOUT", async () => {
  const run = new AbortController()
  const pending = withRowDeadline(10_000, run.signal, (rowSignal) => new Promise((accept) => {
    rowSignal.addEventListener("abort", () => accept({ reason: rowSignal.reason?.code, timedOut: timedOut(rowSignal) }))
  }))
  run.abort()
  assert.deepEqual(await pending, { reason: "INTERRUPTED", timedOut: false })
})

test("probe failures: deadline, request timeout, anything else", () => {
  const live = new AbortController()
  assert.equal(probeFailure({ code: "HTTP_TIMEOUT" }, live.signal), "DISPATCH_TIMEOUT")
  assert.equal(probeFailure(new Error("boom"), live.signal), "DISPATCH_COULD_NOT_RUN")
})

test("server log: password redacted, written to a private file, tail filtered by session", async () => {
  const { mkdtemp, readFile, stat } = await import("node:fs/promises")
  const { tmpdir } = await import("node:os")
  const { join } = await import("node:path")
  const dir = await mkdtemp(join(tmpdir(), "aid-e2e-log-"))
  const log = serverLog(join(dir, "opencode-server.log"))
  log.write("opencode server listening\nserver password s3cr3t-pw\n")
  assert.match(log.raw, /s3cr3t-pw/) // kept in memory only until it's read
  log.forgetRaw()
  log.write("level=INFO session=ses_A started\nlevel=INFO session=ses_B started\nlevel=WARN session=ses_A stuck")
  assert.equal(log.raw, "")
  assert.deepEqual(log.tail(["ses_A"]), ["level=INFO session=ses_A started", "level=WARN session=ses_A stuck"])
  assert.equal(log.tail(["ses_none"], 2).length, 2) // no match: the last lines overall
  await log.close()
  const text = await readFile(join(dir, "opencode-server.log"), "utf8")
  assert.doesNotMatch(text, /s3cr3t-pw/)
  assert.match(text, /server password \[redacted\]/)
  assert.match(text, /ses_A stuck/) // the trailing partial line is flushed on close
  if (process.platform !== "win32") assert.equal((await stat(join(dir, "opencode-server.log"))).mode & 0o777, 0o600)
})

test("the orchestrator expects its profile's variant, as it ran live on OpenCode 2.2.0", () => {
  // The user's live case: aid-orchestrator → openai/gpt-6-astra#medium; its
  // primary turns ran on gpt-6-astra:medium.
  const resolved = { profiles: { lead: { model: "openai/gpt-6-astra#medium" }, fast: { model: "openai/gpt-6-luna#high" } },
    agents: { "aid-orchestrator": "lead", "aid-explorer": "fast" } }
  const { rows } = planRows(resolved)
  assert.deepEqual(expectations(rows, [
    { providerID: "openai", id: "gpt-6-astra", variants: ["low", "medium", "high"] },
    { providerID: "openai", id: "gpt-6-luna", variants: ["high"] },
  ]), [])
  const orchestrator = rows.find((r) => r.agent === "aid-orchestrator")
  assert.deepEqual(orchestrator.expect, { providerID: "openai", modelID: "gpt-6-astra", variant: "medium" })
  const observed = { providerID: "openai", id: "gpt-6-astra", variant: "medium" }
  assert.deepEqual(compareModels(orchestrator.expect, { parent_assistant_0: observed, parent_assistant_1: observed }), { state: "PASS", codes: [] })
  // A variant the catalog doesn't offer is still stripped, for the primary too.
  const { rows: bare } = planRows(resolved)
  expectations(bare, [{ providerID: "openai", id: "gpt-6-astra", variants: ["low"] }, { providerID: "openai", id: "gpt-6-luna" }])
  assert.deepEqual(bare.find((r) => r.agent === "aid-orchestrator").expect, { providerID: "openai", modelID: "gpt-6-astra" })
})

test("a probe's parent session carries the chosen model as OpenCode 2's Model.Ref", () => {
  assert.deepEqual(probeSessionBody("/r"), { agent: "aid-orchestrator", location: { directory: "/r" } })
  assert.deepEqual(probeSessionBody("/r", { providerID: "openai", modelID: "gpt-6-luna", variant: "high" }), {
    agent: "aid-orchestrator", location: { directory: "/r" }, model: { providerID: "openai", id: "gpt-6-luna", variant: "high" },
  })
  assert.deepEqual(probeSessionBody("/r", { providerID: "acme", modelID: "x" }).model, { providerID: "acme", id: "x" })
  assert.equal("model" in probeSessionBody("/r", { providerID: "" }), false)
})

test("row lines name the parent model and break down the time", () => {
  const row = { agent: "aid-creator", mode: "subagent", expect: { providerID: "openai", modelID: "gpt-6-astra", variant: "medium" } }
  const line = rowLine(row, { state: "PASS", codes: [] }, { registry: { providerID: "openai", id: "gpt-6-astra", variant: "medium" } }, 12200, {
    parentModel: { providerID: "openai", id: "gpt-6-luna", variant: "high" }, timing: { parent: 4100, child: 6000, other: 2100 },
  })
  assert.match(line, /parent=openai\/gpt-6-luna:high/)
  assert.match(line, /\(12\.2s: parent 4\.1s, child 6\.0s, other 2\.1s\)$/)
  assert.match(rowLine(row, { state: "PASS", codes: [] }, {}, 3000), /\(3\.0s\)$/)
})

test("jsonc: comments and trailing commas go, strings (URLs) stay", () => {
  const text = `{
    // providers
    "providers": { "p": { "options": { "baseURL": "https://x.example/v1", "note": "a /* not */ b" } }, },
    /* block */ "plugins": ["/somewhere/clone"],
  }`
  assert.deepEqual(JSON.parse(stripJsonComments(text)), {
    providers: { p: { options: { baseURL: "https://x.example/v1", note: "a /* not */ b" } } }, plugins: ["/somewhere/clone"],
  })
  const isolated = isolatedGlobalConfig(text)
  assert.equal("plugins" in isolated, false)
  assert.equal(isolated.providers.p.options.baseURL, "https://x.example/v1")
  assert.throws(() => isolatedGlobalConfig("[1]"), /GLOBAL_OPENCODE_CONFIG_SCHEMA/)
})

test("the suite's server sees the user's global config without plugins, and nothing else can add one", async () => {
  const { mkdtemp, readFile, mkdir, writeFile, stat } = await import("node:fs/promises")
  const { tmpdir } = await import("node:os")
  const { join } = await import("node:path")
  const user = await mkdtemp(join(tmpdir(), "aid-e2e-user-"))
  const root = await mkdtemp(join(tmpdir(), "aid-e2e-root-"))
  await mkdir(join(user, "opencode"), { recursive: true })
  await writeFile(join(user, "opencode", "opencode.json"), JSON.stringify({ plugins: ["/clone"], providers: { p: { name: "P" } }, model: "p/m" }))
  const env = await isolatedServerEnv(root, { XDG_CONFIG_HOME: user, OPENCODE_CONFIG: "/elsewhere.json", OPENCODE_CONFIG_DIR: "/elsewhere", KEEP: "1" })
  assert.equal(env.XDG_CONFIG_HOME, join(root, "xdg-config"))
  assert.equal(env.OPENCODE_CONFIG, undefined)
  assert.equal(env.OPENCODE_CONFIG_DIR, undefined)
  assert.equal(env.KEEP, "1")
  const copied = JSON.parse(await readFile(join(root, "xdg-config", "opencode", "opencode.json"), "utf8"))
  assert.deepEqual(copied, { providers: { p: { name: "P" } }, model: "p/m" })
  if (process.platform !== "win32") assert.equal((await stat(join(root, "xdg-config", "opencode", "opencode.json"))).mode & 0o777, 0o600)
  // No global config at all: an empty scratch config dir, still isolated.
  const bare = await mkdtemp(join(tmpdir(), "aid-e2e-root-"))
  const env2 = await isolatedServerEnv(bare, { XDG_CONFIG_HOME: join(user, "nothing-here") })
  assert.equal(env2.XDG_CONFIG_HOME, join(bare, "xdg-config"))
})

test("registration requires exactly one aid plugin, loaded from the checkout", () => {
  const same = (p) => p
  const local = (path, id = "ai-development") => ({ id, source: { type: "local", path }, state: { status: "active" } })
  assert.deepEqual(pluginFromCheckout([local("/repo")], "/repo", same), { ok: true, paths: ["/repo"] })
  assert.equal(pluginFromCheckout([local("/repo/index.js")], "/repo", same).ok, true)
  // The user's real case: a clone listed in the global config was loaded instead.
  assert.deepEqual(pluginFromCheckout([local("/home/u/.local/share/ai-development")], "/repo", same), { ok: false, paths: ["/home/u/.local/share/ai-development"] })
  assert.equal(pluginFromCheckout([local("/repo"), local("/clone")], "/repo", same).ok, false)
  assert.equal(pluginFromCheckout([{ id: "ai-development", source: { type: "package", target: "ai-development" } }], "/repo", same).ok, false)
  assert.equal(pluginFromCheckout([], "/repo", same).ok, false)
  assert.equal(pluginFromCheckout([local("/other", "someone-else")], "/repo", same).ok, false)
})
