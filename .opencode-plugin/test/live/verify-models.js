// Pure model-identity verification, ported from the live evidence verifier.
export const SEVERITY = Object.freeze({ PASS: 0, UNVERIFIED: 1, FAIL: 2 })

export function statusMax(left, right) { return SEVERITY[right] > SEVERITY[left] ? right : left }

export function measured(probe, category) {
  if (!probe.measured.has(category)) probe.categories[category] = "PASS"
  probe.measured.add(category)
}

export function mark(result, probe, category, state, code) {
  probe.measured.add(category)
  probe.categories[category] = statusMax(probe.categories[category], state)
  probe.state = statusMax(probe.state, state)
  if (!probe.assertions.includes(code)) probe.assertions.push(code)
  if (state === "FAIL") result.failed = true
  if (state === "UNVERIFIED") result.incomplete = true
}

export function globalMark(result, state, code) {
  result.checks.push({ state, code })
  if (state === "FAIL") result.failed = true
  if (state === "UNVERIFIED") result.incomplete = true
}

export function modelTuple(data, variantAllowed = true) {
  if (!data || typeof data !== "object") return { valid: false, providerID: null, modelID: null, ...(variantAllowed ? { variant: null } : {}) }
  const providerID = data.providerID
  const modelID = data.modelID ?? data.id
  return { valid: typeof providerID === "string" && providerID.length > 0 && typeof modelID === "string" && modelID.length > 0,
    providerID: typeof providerID === "string" ? providerID : null, modelID: typeof modelID === "string" ? modelID : null,
    // OpenCode's model resolver maps variantID === "default" to undefined.
    ...(variantAllowed ? { variant: typeof data.variant === "string" && data.variant.length > 0 && data.variant !== "default" ? data.variant : null } : {}) }
}

export function inspectSubagentCalls(messages, agent) {
  const unverified = (code) => ({ state: "UNVERIFIED", codes: [code] })
  try {
    const calls = []
    for (const message of Array.isArray(messages) ? messages : []) {
      for (const part of Array.isArray(message?.content) ? message.content : []) {
        if (part?.type === "tool" && part.name === "subagent") calls.push(part)
      }
    }
    if (!calls.length) return unverified("SUBAGENT_CALL_MISSING")
    if (calls.length > 1) return unverified("SUBAGENT_CALL_AMBIGUOUS")
    const input = calls[0].state?.input
    if (!input || typeof input !== "object" || Array.isArray(input)) return unverified("SUBAGENT_CALL_MISSING")
    if (input.agent !== agent) return unverified("SUBAGENT_CALL_AGENT_MISMATCH")
    if (typeof input.model === "string" && input.model.length > 0) return unverified("SUBAGENT_MODEL_OVERRIDE")
    return { state: "PASS", codes: [] }
  } catch {
    return unverified("SUBAGENT_CALL_MISSING")
  }
}

/**
 * Model evidence for a primary agent's session. OpenCode resolves a primary
 * turn from the session's stored model and, when none is stored, from the
 * default model; the agent's own `model` is not consulted. A session created
 * without a model (as the suite does) therefore has no stored model by
 * design, so it is evidence only when present. The assistant messages record
 * the model each turn actually used and are required.
 */
export function primarySessionEvidence(session, messages) {
  const sources = {}
  const notes = []
  if (session && typeof session === "object" && session.model !== undefined && session.model !== null) {
    sources.parent_session = session.model
  }
  if (!Array.isArray(messages)) {
    notes.push("PARENT_CONTEXT_UNAVAILABLE")
    return { sources, notes }
  }
  const assistants = messages.filter((message) => message?.type === "assistant")
  if (!assistants.length) notes.push("PARENT_ASSISTANT_MESSAGE_MISSING")
  assistants.forEach((message, index) => { sources[`parent_assistant_${index}`] = message.model })
  return { sources, notes }
}

export function compareModels(expected, sources) {
  const result = { failed: false, incomplete: false }
  const probe = { state: "PASS", categories: { model_identity: "UNVERIFIED" }, assertions: [], measured: new Set() }
  const verdict = () => ({ state: probe.state, codes: probe.assertions })
  const note = (state, code) => mark(result, probe, "model_identity", state, code)
  measured(probe, "model_identity")
  const tuples = Object.values(sources ?? {}).map((value) => modelTuple(value))
  // No evidence is never a pass (including an entirely invalid source set).
  if (!tuples.length || tuples.some((tuple) => !tuple.valid)) {
    note("UNVERIFIED", "MODEL_PROVIDER_OR_ID_MISSING")
    return verdict()
  }
  const providerModels = tuples.map((tuple) => `${tuple.providerID}\0${tuple.modelID}`)
  if (new Set(providerModels).size !== 1) {
    note("FAIL", "MODEL_PROVIDER_OR_ID_MISMATCH")
    return verdict()
  }
  const [providerID, modelID] = providerModels[0].split("\0")
  const expectation = modelTuple(expected)
  if (expectation.providerID !== providerID || expectation.modelID !== modelID) {
    note("FAIL", "MODEL_NOT_AN_EXPECTED_CANDIDATE")
    return verdict()
  }
  const variantSources = tuples.map((tuple) => tuple.variant)
  if (expectation.variant !== null) {
    if (variantSources.some((value) => value === null)) note("UNVERIFIED", "EXPECTED_VARIANT_MISSING")
    const observedVariants = variantSources.filter((value) => value !== null)
    if (new Set(observedVariants).size > 1 || observedVariants.some((value) => value !== expectation.variant)) note("FAIL", "VARIANT_MISMATCH")
  } else if (new Set(variantSources.filter(Boolean)).size > 1) {
    note("FAIL", "VARIANT_CONFLICT")
  } else if (variantSources.some(Boolean)) {
    note("UNVERIFIED", "VARIANT_SELECTION_NOT_CONFIGURED")
  }
  return verdict()
}

export function summarize(results) {
  const summary = { failed: false, incomplete: false, checks: [] }
  const entries = Array.isArray(results) ? results : Object.values(results ?? {})
  if (!entries.length) globalMark(summary, "UNVERIFIED", "MODEL_PROVIDER_OR_ID_MISSING")
  for (const result of entries) {
    globalMark(summary, result?.state ?? "UNVERIFIED", null)
    if (result?.failed) summary.failed = true
    if (result?.incomplete) summary.incomplete = true
  }
  const state = summary.failed ? "FAIL" : summary.incomplete ? "UNVERIFIED" : "PASS"
  return { state, failed: summary.failed, incomplete: summary.incomplete, exitCode: summary.failed ? 1 : summary.incomplete ? 2 : 0 }
}
