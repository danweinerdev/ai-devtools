import assert from "node:assert/strict"
import test from "node:test"

import { activeSubagents, formatModel, readActiveSubagents, reuseResult, reuseRows } from "../src/active-subagents.js"

function session(id, created = 1, parentID = "root") {
  return { id, parentID, agent: id, model: { providerID: "provider", id: "model" }, time: { created } }
}

function dataFor(sessions, running = []) {
  return {
    root: () => "root",
    family: () => ["root", ...Object.keys(sessions).filter((id) => id !== "root")],
    get: (id) => sessions[id],
    status: (id) => running.includes(id) ? "running" : "idle",
  }
}

function row(sessionID, agent = sessionID, model = "provider/model") {
  return { sessionID, agent, model }
}

test("activeSubagents returns only running sessions and drops newly idle sessions", () => {
  const running = ["worker"]
  const data = dataFor({ worker: session("worker"), idle: session("idle") }, running)
  assert.deepEqual(activeSubagents("root", data), [row("worker")])
  running.pop()
  assert.deepEqual(activeSubagents("root", data), [])
  running.push("idle")
  assert.deepEqual(activeSubagents("root", data), [row("idle")])
})

test("activeSubagents excludes the root even when it is running", () => {
  const data = dataFor({ root: session("root", 0, undefined), worker: session("worker") }, ["root", "worker"])
  assert.deepEqual(activeSubagents("root", data), [row("worker")])
})

test("activeSubagents resolves the conversation root when viewing a child", () => {
  const data = dataFor({ viewed: session("viewed"), sibling: session("sibling") }, ["root", "viewed", "sibling"])
  data.root = (id) => {
    assert.equal(id, "viewed")
    return "root"
  }
  data.family = (id) => {
    assert.equal(id, "viewed")
    return ["root", "viewed", "sibling"]
  }
  assert.deepEqual(activeSubagents("viewed", data), [row("sibling"), row("viewed")])
})

test("activeSubagents includes running grandchildren", () => {
  const data = dataFor({ child: session("child"), grandchild: session("grandchild", 2, "child") }, ["grandchild"])
  assert.deepEqual(activeSubagents("root", data), [row("grandchild")])
})

test("activeSubagents sorts by creation time then session ID without changing the family", () => {
  const family = ["root", "late", "b", "a", "early"]
  const data = dataFor({ late: session("late", 3), b: session("b", 2), a: session("a", 2), early: session("early", 1) }, family)
  data.family = () => family
  assert.deepEqual(activeSubagents("root", data), [row("early"), row("a"), row("b"), row("late")])
  assert.deepEqual(family, ["root", "late", "b", "a", "early"])
})

test("formatModel includes a selected variant", () => {
  assert.equal(formatModel({ providerID: "provider", id: "model", variant: "high" }), "provider/model [high]")
})

test("formatModel omits default, empty, missing, and non-string variants", () => {
  for (const variant of ["default", "", undefined, null, 42]) {
    assert.equal(formatModel({ providerID: "provider", id: "model", variant }), "provider/model")
  }
  assert.equal(formatModel({ providerID: "provider", id: "model" }), "provider/model")
})

test("formatModel reports unknown when either model identity field is missing", () => {
  for (const model of [undefined, null, {}, { id: "model" }, { providerID: "provider" },
    { providerID: "", id: "model" }, { providerID: "provider", id: "" }, { providerID: 42, id: "model" }]) {
    assert.equal(formatModel(model), "model unknown")
  }
})

test("formatModel never throws on throwing model fields", () => {
  for (const field of ["providerID", "id", "variant"]) {
    const model = { providerID: "provider", id: "model" }
    Object.defineProperty(model, field, { get() { throw new Error("unavailable") } })
    assert.equal(formatModel(model), "model unknown")
  }
})

test("activeSubagents uses safe labels for missing session info", () => {
  const data = dataFor({ missing: undefined }, ["missing"])
  assert.deepEqual(activeSubagents("root", data), [row("missing", "unknown agent", "model unknown")])
})

test("activeSubagents tolerates missing fields and invalid family IDs", () => {
  const data = dataFor({ missing: {}, invalid: { agent: "", time: { created: NaN } } }, ["missing", "invalid"])
  data.family = () => ["root", undefined, null, 42, "", "missing", "invalid"]
  assert.deepEqual(activeSubagents("root", data), [
    row("invalid", "unknown agent", "model unknown"),
    row("missing", "unknown agent", "model unknown"),
  ])
})

test("activeSubagents returns an empty list for non-array families", () => {
  for (const family of [undefined, null, {}, "worker", 42]) {
    const data = dataFor({ worker: session("worker") }, ["worker"])
    data.family = () => family
    assert.deepEqual(activeSubagents("root", data), [])
  }
})

test("activeSubagents never throws when a session accessor throws", () => {
  for (const accessor of ["root", "family", "get", "status"]) {
    const data = dataFor({ worker: session("worker") }, ["worker"])
    data[accessor] = () => { throw new Error("unavailable") }
    assert.doesNotThrow(() => activeSubagents("root", data))
  }
})

test("readActiveSubagents reports no error when every read succeeds", () => {
  const data = dataFor({ worker: session("worker") }, ["worker"])
  assert.deepEqual(readActiveSubagents("root", data), { rows: [row("worker")], error: undefined })
})

test("readActiveSubagents reports a conversation-level failure instead of an empty list", () => {
  for (const accessor of ["root", "family"]) {
    const data = dataFor({ worker: session("worker") }, ["worker"])
    data[accessor] = () => { throw new Error(`${accessor} broke`) }
    assert.deepEqual(readActiveSubagents("root", data), { rows: [], error: `${accessor} broke` })
  }
})

test("readActiveSubagents distinguishes broken data from nothing running", () => {
  for (const data of [undefined, null, {}, { root: () => "root" }]) {
    const result = readActiveSubagents("root", data)
    assert.deepEqual(result.rows, [])
    assert.equal(typeof result.error, "string")
    assert.ok(result.error.length > 0)
  }
  assert.deepEqual(readActiveSubagents("root", { root: () => undefined, family: () => ["root"] }),
    { rows: [], error: "conversation root is unavailable" })
  assert.deepEqual(readActiveSubagents("root", { root: () => "root", family: () => "nope" }),
    { rows: [], error: "conversation sessions are unavailable" })
  // Genuinely idle is the only way to get no rows and no error.
  assert.deepEqual(readActiveSubagents("root", dataFor({ worker: session("worker") }, [])), { rows: [], error: undefined })
})

test("readActiveSubagents keeps healthy sessions when one session's status cannot be read", () => {
  const data = dataFor({ good: session("good", 1), bad: session("bad", 2) }, ["good", "bad"])
  const status = data.status
  data.status = (id) => {
    if (id === "bad") throw new Error("status unreadable")
    return status(id)
  }
  assert.deepEqual(readActiveSubagents("root", data), { rows: [row("good")], error: "status unreadable" })
})

test("readActiveSubagents lists a running session whose info cannot be read", () => {
  const data = dataFor({ good: session("good", 1), bad: session("bad", 2) }, ["good", "bad"])
  const get = data.get
  data.get = (id) => {
    if (id === "bad") throw new Error("info unreadable")
    return get(id)
  }
  const result = readActiveSubagents("root", data)
  assert.equal(result.error, "info unreadable")
  // The unreadable session has created 0, so it sorts ahead of "good".
  assert.deepEqual(result.rows, [row("bad", "unknown agent", "model unknown"), row("good")])
})

for (const field of ["agent", "time", "time.created", "model"]) {
  test(`readActiveSubagents tolerates a throwing ${field} getter without affecting healthy sessions`, () => {
    const bad = session("bad", 2)
    const target = field === "time.created" ? bad.time : bad
    const property = field === "time.created" ? "created" : field
    Object.defineProperty(target, property, { get() { throw new Error(`${field} unreadable`) } })
    const data = dataFor({ good: session("good", 1), bad }, ["good", "bad"])

    let result
    assert.doesNotThrow(() => { result = readActiveSubagents("root", data) })
    assert.equal(result.error, `${field} unreadable`)
    // A failed info read resets created to 0, so "bad" sorts ahead of "good".
    assert.deepEqual(result.rows, [row("bad", "unknown agent", "model unknown"), row("good")])
  })
}

test("readActiveSubagents keeps the first error and bounds its length to one line", () => {
  const data = dataFor({ a: session("a", 1), b: session("b", 2) }, ["a", "b"])
  data.status = (id) => { throw new Error(id === "a" ? `first\n${"x".repeat(300)}` : "second") }
  const { rows, error } = readActiveSubagents("root", data)
  assert.deepEqual(rows, [])
  assert.ok(error.startsWith("first x"))
  assert.ok(!error.includes("\n"))
  assert.ok(error.length <= 120)
})

test("readActiveSubagents never reads the root's status", () => {
  const statusCalls = []
  const getCalls = []
  const data = dataFor({ worker: session("worker"), idle: session("idle") }, ["root", "worker"])
  const status = data.status
  const get = data.get
  data.status = (id) => { statusCalls.push(id); return status(id) }
  data.get = (id) => { getCalls.push(id); return get(id) }
  readActiveSubagents("root", data)
  assert.ok(!statusCalls.includes("root"), "the root's status must not be read, or the memo re-runs whenever the main agent starts or stops")
  assert.deepEqual(getCalls, ["worker"], "info is read only for running sessions")
})

test("activeSubagents returns an empty list for undefined data or missing accessors", () => {
  for (const data of [undefined, null, {}, { root: () => "root" }, { root: () => undefined, family: () => ["root"] }]) {
    assert.deepEqual(activeSubagents("root", data), [])
  }
})

test("reuseRows returns the previous array when nothing visible changed", () => {
  const previous = [row("a"), row("b")]
  const next = [row("a"), row("b")]
  assert.equal(reuseRows(previous, next), previous)
})

test("reuseRows keeps unchanged row objects when a sub-agent starts", () => {
  const a = row("a")
  const b = row("b")
  const rows = reuseRows([a, b], [row("a"), row("b"), row("c")])
  assert.equal(rows[0], a)
  assert.equal(rows[1], b)
  assert.deepEqual(rows[2], row("c"))
})

test("reuseRows keeps the remaining row objects when a sub-agent in the middle finishes", () => {
  const a = row("a")
  const b = row("b")
  const c = row("c")
  const rows = reuseRows([a, b, c], [row("a"), row("c")])
  assert.equal(rows.length, 2)
  assert.equal(rows[0], a)
  assert.equal(rows[1], c)
})

test("reuseRows replaces a row whose agent or model changed", () => {
  const a = row("a")
  const b = row("b")
  for (const changed of [row("a", "other agent"), row("a", "a", "provider/other")]) {
    const rows = reuseRows([a, b], [changed, row("b")])
    assert.notEqual(rows[0], a)
    assert.deepEqual(rows[0], changed)
    assert.equal(rows[1], b)
  }
})

test("reuseRows returns a new array when only the order changed", () => {
  const a = row("a")
  const b = row("b")
  const previous = [a, b]
  const rows = reuseRows(previous, [row("b"), row("a")])
  assert.notEqual(rows, previous)
  assert.equal(rows[0], b)
  assert.equal(rows[1], a)
})

test("reuseRows handles empty and invalid input", () => {
  const next = [row("a")]
  assert.equal(reuseRows(undefined, next), next)
  assert.equal(reuseRows([], next), next)
  assert.deepEqual(reuseRows([row("a")], []), [])
  assert.deepEqual(reuseRows([row("a")], undefined), [])
  const empty = []
  assert.equal(reuseRows(empty, []), empty)
})

test("reuseResult returns the previous result when rows and error are unchanged", () => {
  const previous = { rows: [row("a")], error: undefined }
  assert.equal(reuseResult(previous, { rows: [row("a")], error: undefined }), previous)
  const failed = { rows: [], error: "broke" }
  assert.equal(reuseResult(failed, { rows: [], error: "broke" }), failed)
})

test("reuseResult returns a new result when the error changes but keeps unchanged rows", () => {
  const a = row("a")
  const previous = { rows: [a], error: undefined }
  const result = reuseResult(previous, { rows: [row("a")], error: "incomplete" })
  assert.notEqual(result, previous)
  assert.equal(result.rows, previous.rows)
  assert.equal(result.error, "incomplete")
})

test("reuseResult works without a previous result", () => {
  const next = { rows: [row("a")], error: undefined }
  assert.deepEqual(reuseResult(undefined, next), next)
})

test("readActiveSubagents and reuseResult together keep identity across an unchanged recomputation", () => {
  const data = dataFor({ worker: session("worker"), other: session("other", 2) }, ["worker", "other"])
  const first = reuseResult(undefined, readActiveSubagents("root", data))
  assert.equal(reuseResult(first, readActiveSubagents("root", data)), first)
})
