// The live suite's offline half: planning rows from aid's config, the scratch
// config it writes, expectations from the catalog, and the guard verdict.
// None of this calls a model or starts OpenCode.
import assert from "node:assert/strict"
import test from "node:test"

import { defaultConfig, expectations, judgeGuard, parseArgs, planRows, probeFailure, scratchConfig, serverLog, timedOut, withRowDeadline } from "../live/e2e.js"

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
