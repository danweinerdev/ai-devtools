// The live suite's offline half: planning rows from aid's config, the scratch
// config it writes, expectations from the catalog, and the guard verdict.
// None of this calls a model or starts OpenCode.
import assert from "node:assert/strict"
import test from "node:test"

import { expectations, judgeGuard, parseArgs, planRows, scratchConfig } from "../live/e2e.js"

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

test("expectations strip variants the catalog lacks, drop them for the primary, and flag shared models", () => {
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
  assert.equal(by["aid-explorer"].shared, true)
  assert.equal(by["aid-oracle"].shared, false)
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
