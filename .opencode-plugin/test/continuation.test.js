import assert from "node:assert/strict"
import test from "node:test"

import { originalAgentFromHistory } from "../src/continuation.js"
import { createSetup } from "../src/setup.js"
import { fakeAid, fakeContext, ok } from "./fake-opencode.js"

const ORCHESTRATOR = "aid-orchestrator"
const BLOCKED = /dispatched as aid-mechanic; it can't be continued as aid-oracle.*Continue it as aid-mechanic, or start a new aid-oracle session/s

function guardAid() {
  return fakeAid({ "guard opencode-tool": ok(JSON.stringify({ decision: "allow" })) })
}

async function boot({ aid = guardAid().aid, history, historyTimeoutMs } = {}) {
  const fake = fakeContext()
  if (history !== undefined) fake.ctx.session.context = history
  const dispose = await createSetup({ aid, historyTimeoutMs, log: () => {} })(fake.ctx)
  const before = (input) => fake.hooks.tool["execute.before"]({ tool: "subagent", input, sessionID: "parent", agent: ORCHESTRATOR })
  const after = (input, metadata, status = "completed") =>
    fake.hooks.tool["execute.after"]({ tool: "subagent", input, sessionID: "parent", agent: ORCHESTRATOR, status, result: { output: "", metadata } })
  return { ...fake, dispose, before, after }
}

/** Dispatch a new child as `agent`, as OpenCode runs it: before, then the result naming the child. */
async function dispatch(env, agent, child, { background = false } = {}) {
  const input = { agent, prompt: "work", description: "work", ...(background ? { background: true } : {}) }
  await env.before(input)
  await env.after(input, { sessionID: child, status: background ? "running" : "completed" })
}

const continueAs = (env, agent, sessionID) => env.before({ agent, prompt: "more", description: "more", sessionID })
const tick = () => new Promise((resolve) => setImmediate(resolve))

test("continuing a child session as a different agent is blocked, before aid is asked", async () => {
  const { aid, calls } = guardAid()
  const env = await boot({ aid })
  await dispatch(env, "aid-mechanic", "child-1")
  const guardCalls = () => calls.filter((c) => c.args.join(" ") === "guard opencode-tool").length
  const before = guardCalls()
  await assert.rejects(continueAs(env, "aid-oracle", "child-1"), BLOCKED)
  assert.equal(guardCalls(), before, "a blocked continuation never reaches aid")
  await env.dispose()
})

test("continuing a child session as the same agent is allowed", async () => {
  const env = await boot()
  await dispatch(env, "aid-mechanic", "child-1")
  await continueAs(env, "aid-mechanic", "child-1")
  // The continuation's own result doesn't redefine the child.
  await env.after({ agent: "aid-mechanic", sessionID: "child-1" }, { sessionID: "child-1", status: "completed" })
  await assert.rejects(continueAs(env, "aid-oracle", "child-1"), BLOCKED)
  await env.dispose()
})

test("a background dispatch is recorded from its running result", async () => {
  const env = await boot()
  await dispatch(env, "aid-mechanic", "child-bg", { background: true })
  await assert.rejects(continueAs(env, "aid-oracle", "child-bg"), BLOCKED)
  await env.dispose()
})

test("the first dispatch evidence wins", async () => {
  const env = await boot()
  await dispatch(env, "aid-mechanic", "child-1")
  await env.after({ agent: "aid-oracle", prompt: "x" }, { sessionID: "child-1", status: "completed" })
  await continueAs(env, "aid-mechanic", "child-1")
  await assert.rejects(continueAs(env, "aid-oracle", "child-1"), BLOCKED)
  await env.dispose()
})

test("a dispatch that failed records nothing, and a new session or no sessionID is never checked", async () => {
  const env = await boot()
  const input = { agent: "aid-mechanic", prompt: "work" }
  await env.before(input)
  await env.hooks.tool["execute.after"]({ tool: "subagent", input, sessionID: "parent", agent: ORCHESTRATOR, status: "error", error: { message: "boom" } })
  await continueAs(env, "aid-oracle", "child-1")
  await env.before({ agent: "aid-oracle", prompt: "fresh", sessionID: "" })
  await env.dispose()
})

test("an unseen dispatch is recovered from the session's context history", async () => {
  const histories = {
    "from-assistant": [{ type: "user", text: "hi" }, { type: "assistant", agent: "aid-mechanic" }, { type: "assistant", agent: "aid-oracle" }],
    "from-switch": [{ type: "agent-switched", agent: "aid-oracle", previous: "aid-mechanic" }, { type: "assistant", agent: "aid-oracle" }],
    "from-initial-switch": [{ type: "agent-switched", agent: "aid-mechanic" }, { type: "assistant", agent: "aid-oracle" }],
  }
  const asked = []
  const env = await boot({
    history: async ({ sessionID }) => {
      asked.push(sessionID)
      return { data: histories[sessionID] }
    },
  })
  for (const sessionID of Object.keys(histories)) {
    await assert.rejects(continueAs(env, "aid-oracle", sessionID), BLOCKED, sessionID)
    await continueAs(env, "aid-mechanic", sessionID)
  }
  assert.deepEqual(asked, Object.keys(histories), "a recovered agent is remembered, so history is read once per session")
  await env.dispose()
})

test("unknown identity fails open: compaction, no agent, lookup failure, timeout, no context API", async () => {
  const cases = {
    compaction: async () => [{ type: "compaction" }, { type: "assistant", agent: "aid-mechanic" }],
    "no agent yet": async () => [{ type: "user", text: "hi" }],
    "not a list": async () => ({ data: null }),
    "lookup throws": () => {
      throw new Error("no such session")
    },
    "lookup rejects": async () => {
      throw new Error("server down")
    },
    "lookup hangs": () => new Promise(() => {}),
  }
  for (const [name, history] of Object.entries(cases)) {
    const env = await boot({ history, historyTimeoutMs: 20 })
    await continueAs(env, "aid-oracle", "child-x").catch((error) => assert.fail(`${name}: ${error.message}`))
    await env.dispose()
  }
  const env = await boot()
  await continueAs(env, "aid-oracle", "child-x")
  await env.dispose()
})

test("session.deleted forgets the child, and dispose forgets everything", async () => {
  const env = await boot()
  await dispatch(env, "aid-mechanic", "child-1")
  await dispatch(env, "aid-mechanic", "child-2")
  env.emit({ type: "session.deleted", data: { sessionID: "child-1" } })
  await tick()
  await continueAs(env, "aid-oracle", "child-1")
  await assert.rejects(continueAs(env, "aid-oracle", "child-2"), BLOCKED)
  env.emit({ type: "session.deleted", properties: { info: { id: "child-2" } } })
  await tick()
  await continueAs(env, "aid-oracle", "child-2")
  await dispatch(env, "aid-mechanic", "child-3")
  await env.dispose()
  await continueAs(env, "aid-oracle", "child-3")
})

test("the guard holds when aid is missing, and other tools are untouched", async () => {
  const env = await boot({ aid: fakeAid({}).aid })
  await dispatch(env, "aid-mechanic", "child-1")
  await assert.rejects(continueAs(env, "aid-oracle", "child-1"), BLOCKED)
  await env.hooks.tool["execute.before"]({ tool: "shell", input: { sessionID: "child-1", agent: "aid-oracle" }, sessionID: "parent", agent: ORCHESTRATOR })
  await env.dispose()
})

test("originalAgentFromHistory reads the first decisive message", () => {
  assert.equal(originalAgentFromHistory(undefined), undefined)
  assert.equal(originalAgentFromHistory([null, { type: "assistant", agent: "a" }]), "a")
  assert.equal(originalAgentFromHistory([{ type: "agent-switched", previous: "", agent: "b" }]), "b")
  assert.equal(originalAgentFromHistory([{ type: "assistant", agent: "" }]), undefined)
})
