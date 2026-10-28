import assert from "node:assert/strict"
import test from "node:test"

import { SEVERITY, compareModels, globalMark, inspectSubagentCalls, mark, measured, modelTuple, primarySessionEvidence, statusMax, summarize } from "../live/verify-models.js"

const expected = { providerID: "openai", modelID: "gpt-6-luna" }
const observed = { providerID: "openai", id: "gpt-6-luna" }
const subagentCall = (input) => ({ type: "tool", name: "subagent", state: { input } })

test("e2e a single subagent call without a model override passes", () => {
  assert.deepEqual(inspectSubagentCalls([{ content: [subagentCall({ agent: "oracle" })] }], "oracle"), { state: "PASS", codes: [] })
})

test("e2e no subagent call is unverified", () => {
  assert.deepEqual(inspectSubagentCalls([{ content: [{ type: "tool", name: "other" }, { type: "text", name: "subagent" }] }], "oracle"), {
    state: "UNVERIFIED", codes: ["SUBAGENT_CALL_MISSING"],
  })
})

test("e2e multiple subagent calls are ambiguous", () => {
  const call = subagentCall({ agent: "oracle" })
  assert.deepEqual(inspectSubagentCalls([{ content: [call] }, { content: [call] }], "oracle"), {
    state: "UNVERIFIED", codes: ["SUBAGENT_CALL_AMBIGUOUS"],
  })
})

test("e2e a subagent call for another agent is unverified", () => {
  assert.deepEqual(inspectSubagentCalls([{ content: [subagentCall({ agent: "extractor" })] }], "oracle"), {
    state: "UNVERIFIED", codes: ["SUBAGENT_CALL_AGENT_MISMATCH"],
  })
})

test("e2e a subagent model override does not exercise routing", () => {
  assert.deepEqual(inspectSubagentCalls([{ content: [subagentCall({ agent: "oracle", model: "openai/gpt-6-sol" })] }], "oracle"), {
    state: "UNVERIFIED", codes: ["SUBAGENT_MODEL_OVERRIDE"],
  })
})

test("e2e an empty subagent model string is not an override", () => {
  assert.deepEqual(inspectSubagentCalls([{ content: [subagentCall({ agent: "oracle", model: "" })] }], "oracle"), { state: "PASS", codes: [] })
})

test("e2e subagent arguments must be readable at state.input", () => {
  for (const part of [
    { type: "tool", name: "subagent", input: { agent: "oracle" } },
    ...[undefined, null, "{}", 42, []].map(subagentCall),
  ]) {
    assert.deepEqual(inspectSubagentCalls([{ content: [part] }], "oracle"), {
      state: "UNVERIFIED", codes: ["SUBAGENT_CALL_MISSING"],
    })
  }
})

test("e2e malformed subagent messages never throw or pass", () => {
  for (const messages of [undefined, null, 42, {}, "messages", [null, {}, { content: null }, { content: {} }, { content: [null] }]]) {
    assert.doesNotThrow(() => inspectSubagentCalls(messages, "oracle"))
    assert.deepEqual(inspectSubagentCalls(messages, "oracle"), { state: "UNVERIFIED", codes: ["SUBAGENT_CALL_MISSING"] })
  }
})

test("e2e agreeing registry, child, and assistant models pass", () => {
  assert.deepEqual(compareModels(expected, { registry: observed, child_session: observed, assistant_0: observed }), { state: "PASS", codes: [] })
})

test("e2e model tuples accept modelID and V2 id without coercing missing identity", () => {
  assert.deepEqual(modelTuple(observed), { valid: true, ...expected, variant: null })
  assert.deepEqual(modelTuple({ ...observed, modelID: "preferred" }, false), { valid: true, providerID: "openai", modelID: "preferred" })
  for (const value of [undefined, null, 42, {}, { providerID: "", id: "m" }, { providerID: "p", id: 42 }]) {
    assert.equal(modelTuple(value).valid, false)
  }
})

test("e2e a source missing provider or model is unverified", () => {
  for (const missing of [{ id: "gpt-6-luna" }, { providerID: "openai" }, undefined]) {
    assert.deepEqual(compareModels(expected, { registry: observed, assistant_0: missing }), {
      state: "UNVERIFIED", codes: ["MODEL_PROVIDER_OR_ID_MISSING"],
    })
  }
})

test("e2e sources disagreeing on provider or model fail", () => {
  for (const different of [{ ...observed, id: "gpt-6-sol" }, { ...observed, providerID: "other" }]) {
    assert.deepEqual(compareModels(expected, { registry: observed, child_session: different }), {
      state: "FAIL", codes: ["MODEL_PROVIDER_OR_ID_MISMATCH"],
    })
  }
})

test("e2e agreed but unexpected model fails", () => {
  const wrong = { ...observed, id: "gpt-6-sol" }
  assert.deepEqual(compareModels(expected, { registry: wrong, assistant_0: wrong }), {
    state: "FAIL", codes: ["MODEL_NOT_AN_EXPECTED_CANDIDATE"],
  })
})

test("e2e an expected supported variant is preserved", () => {
  const high = { ...observed, variant: "high" }
  assert.deepEqual(compareModels({ ...expected, variant: "high" }, { registry: high, child_session: high, assistant_0: high }), {
    state: "PASS", codes: [],
  })
})

test("e2e missing expected variant is unverified", () => {
  assert.deepEqual(compareModels({ ...expected, variant: "high" }, { registry: { ...observed, variant: "high" }, assistant_0: observed }), {
    state: "UNVERIFIED", codes: ["EXPECTED_VARIANT_MISSING"],
  })
})

test("e2e wrong expected variant fails", () => {
  assert.deepEqual(compareModels({ ...expected, variant: "high" }, { assistant_0: { ...observed, variant: "low" } }), {
    state: "FAIL", codes: ["VARIANT_MISMATCH"],
  })
})

test("e2e unconfigured conflicting variants fail", () => {
  assert.deepEqual(compareModels(expected, { registry: { ...observed, variant: "low" }, assistant_0: { ...observed, variant: "high" } }), {
    state: "FAIL", codes: ["VARIANT_CONFLICT"],
  })
})

test("e2e an unconfigured selected variant is unverified", () => {
  assert.deepEqual(compareModels(expected, { registry: observed, assistant_0: { ...observed, variant: "high" } }), {
    state: "UNVERIFIED", codes: ["VARIANT_SELECTION_NOT_CONFIGURED"],
  })
})

test("e2e default variant normalizes to no variant for observations and expectations", () => {
  assert.equal(modelTuple({ ...observed, variant: "default" }).variant, null)
  assert.deepEqual(compareModels(expected, { registry: observed, assistant_0: { ...observed, variant: "default" } }), { state: "PASS", codes: [] })
  assert.deepEqual(compareModels({ ...expected, variant: "default" }, { assistant_0: observed }), { state: "PASS", codes: [] })
  assert.deepEqual(compareModels({ ...expected, variant: "high" }, { assistant_0: { ...observed, variant: "default" } }), {
    state: "UNVERIFIED", codes: ["EXPECTED_VARIANT_MISSING"],
  })
})

test("e2e stripped unsupported glm variant passes with no observed variant", () => {
  const stripped = { providerID: "openai", modelID: "glm-5.3" }
  assert.deepEqual(compareModels(stripped, { registry: stripped, child_session: stripped, assistant_0: stripped }), { state: "PASS", codes: [] })
  assert.deepEqual(compareModels(stripped, { assistant_0: { ...stripped, variant: "default" } }), { state: "PASS", codes: [] })
})

test("e2e zero or entirely invalid sources are unverified, never pass", () => {
  for (const sources of [{}, undefined, { registry: null, assistant_0: {} }]) {
    assert.deepEqual(compareModels(expected, sources), { state: "UNVERIFIED", codes: ["MODEL_PROVIDER_OR_ID_MISSING"] })
  }
})

test("e2e parent-session evidence verifies a primary agent", () => {
  assert.deepEqual(compareModels(expected, { parent_session: observed, parent_assistant_0: observed }), { state: "PASS", codes: [] })
})

test("e2e absent primary parent evidence remains unverified even with a matching registry", () => {
  assert.deepEqual(compareModels(expected, {}), { state: "UNVERIFIED", codes: ["MODEL_PROVIDER_OR_ID_MISSING"] })
  assert.deepEqual(compareModels(expected, { registry: observed, parent_session: undefined }), {
    state: "UNVERIFIED", codes: ["MODEL_PROVIDER_OR_ID_MISSING"],
  })
})

test("e2e fail outranks missing variant evidence without losing its reason", () => {
  assert.deepEqual(compareModels({ ...expected, variant: "high" }, { registry: observed, assistant_0: { ...observed, variant: "low" } }), {
    state: "FAIL", codes: ["EXPECTED_VARIANT_MISSING", "VARIANT_MISMATCH"],
  })
})

test("e2e severity and measured categories only escalate after measurement", () => {
  assert.ok(SEVERITY.PASS < SEVERITY.UNVERIFIED && SEVERITY.UNVERIFIED < SEVERITY.FAIL)
  assert.equal(statusMax("FAIL", "UNVERIFIED"), "FAIL")
  const result = { failed: false, incomplete: false, checks: [] }
  const probe = { state: "PASS", categories: { model_identity: "UNVERIFIED" }, assertions: [], measured: new Set() }
  measured(probe, "model_identity")
  assert.equal(probe.categories.model_identity, "PASS")
  mark(result, probe, "model_identity", "UNVERIFIED", "MISSING")
  measured(probe, "model_identity")
  assert.equal(probe.categories.model_identity, "UNVERIFIED")
  mark(result, probe, "model_identity", "FAIL", "WRONG")
  mark(result, probe, "model_identity", "UNVERIFIED", "MISSING")
  measured(probe, "model_identity")
  assert.equal(probe.categories.model_identity, "FAIL")
  assert.equal(probe.state, "FAIL")
  assert.deepEqual(probe.assertions, ["MISSING", "WRONG"])
  assert.equal(result.failed, true)
  assert.equal(result.incomplete, true)
  const global = { failed: false, incomplete: false, checks: [] }
  globalMark(global, "UNVERIFIED", "MISSING")
  globalMark(global, "FAIL", "WRONG")
  assert.deepEqual(global, { failed: true, incomplete: true, checks: [{ state: "UNVERIFIED", code: "MISSING" }, { state: "FAIL", code: "WRONG" }] })
})

test("e2e summaries map pass, fail, and unverified to exit 0, 1, and 2", () => {
  assert.deepEqual(summarize([{ state: "PASS" }, { state: "PASS" }]), { state: "PASS", failed: false, incomplete: false, exitCode: 0 })
  assert.deepEqual(summarize([{ state: "PASS" }, { state: "FAIL" }]), { state: "FAIL", failed: true, incomplete: false, exitCode: 1 })
  assert.deepEqual(summarize([{ state: "UNVERIFIED" }, { state: "PASS" }]), { state: "UNVERIFIED", failed: false, incomplete: true, exitCode: 2 })
  assert.deepEqual(summarize([{ state: "UNVERIFIED" }, { state: "FAIL" }]), { state: "FAIL", failed: true, incomplete: true, exitCode: 1 })
  assert.deepEqual(summarize([{ state: "FAIL" }, { state: "UNVERIFIED" }]), { state: "FAIL", failed: true, incomplete: true, exitCode: 1 })
  assert.equal(summarize([]).exitCode, 2)
})

test("e2e one agent failing does not change another agent's state", () => {
  const passing = compareModels(expected, { assistant_0: observed })
  const failing = compareModels(expected, { assistant_0: { ...observed, id: "wrong" } })
  assert.equal(summarize([passing, failing]).exitCode, 1)
  assert.deepEqual(passing, { state: "PASS", codes: [] })
  assert.deepEqual(failing, { state: "FAIL", codes: ["MODEL_NOT_AN_EXPECTED_CANDIDATE"] })
})

test("primary evidence treats an unset session model as not applicable", () => {
  const messages = [{ type: "user" }, { type: "assistant", model: observed }, { type: "assistant", model: observed }]
  for (const session of [{ id: "s" }, { id: "s", model: undefined }, { id: "s", model: null }, undefined]) {
    const evidence = primarySessionEvidence(session, messages)
    assert.deepEqual(evidence, { sources: { parent_assistant_0: observed, parent_assistant_1: observed }, notes: [] })
    assert.deepEqual(compareModels(expected, { registry: observed, ...evidence.sources }), { state: "PASS", codes: [] })
  }
})

test("primary evidence still checks a stored session model", () => {
  const messages = [{ type: "assistant", model: observed }]
  const matching = primarySessionEvidence({ model: observed }, messages)
  assert.deepEqual(matching.sources, { parent_session: observed, parent_assistant_0: observed })
  assert.deepEqual(compareModels(expected, matching.sources), { state: "PASS", codes: [] })

  const switched = primarySessionEvidence({ model: { providerID: "openai", id: "other" } }, messages)
  assert.deepEqual(compareModels(expected, switched.sources), { state: "FAIL", codes: ["MODEL_PROVIDER_OR_ID_MISMATCH"] })

  const malformed = primarySessionEvidence({ model: { providerID: "openai" } }, messages)
  assert.deepEqual(compareModels(expected, malformed.sources), { state: "UNVERIFIED", codes: ["MODEL_PROVIDER_OR_ID_MISSING"] })
})

test("primary evidence requires assistant messages", () => {
  assert.deepEqual(primarySessionEvidence({}, [{ type: "user" }]), { sources: {}, notes: ["PARENT_ASSISTANT_MESSAGE_MISSING"] })
  assert.deepEqual(primarySessionEvidence({}, undefined), { sources: {}, notes: ["PARENT_CONTEXT_UNAVAILABLE"] })
})
