// Model routing, from aid's resolved config (`aid config --harness opencode
// --json`). aid finds and validates .opencode/aid.json; this module never reads
// it. An agent the config doesn't map keeps no model, so it inherits the
// session's. Ported from opencode-model-router's V2 adapter.

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

/** `"provider/model"` plus optional variant -> V2 `Model.Ref`. */
export function modelRef(model, variant) {
  const separator = model.indexOf("/")
  return {
    providerID: model.slice(0, separator),
    id: model.slice(separator + 1),
    ...(variant ? { variant } : {}),
  }
}

/** `"provider/model#variant"` -> `{ model: "provider/model", variant? }`. */
export function splitModel(spec) {
  const hash = spec.indexOf("#")
  return hash === -1 ? { model: spec } : { model: spec.slice(0, hash), ...(spec.slice(hash + 1) ? { variant: spec.slice(hash + 1) } : {}) }
}

/**
 * aid's config -> `{ agent: { model, variant?, reasoningEffort? } }`, one entry
 * per agent mapped to a profile. A profile aid didn't define is skipped: aid
 * validates references, so it can't happen with a real aid.
 */
export function routesFrom(config) {
  const routes = {}
  if (!object(config) || !object(config.agents) || !object(config.profiles)) return routes
  for (const [agent, profileName] of Object.entries(config.agents)) {
    const profile = config.profiles[profileName]
    if (!object(profile) || typeof profile.model !== "string" || !profile.model.includes("/")) continue
    routes[agent] = {
      ...splitModel(profile.model),
      ...(profile.reasoning_effort !== undefined ? { reasoningEffort: profile.reasoning_effort } : {}),
    }
  }
  return routes
}

/** Report unsupported variants only for models already present in the catalog. */
export function unsupportedVariants(routes, catalog) {
  const findings = []
  try {
    if (!object(routes) || !Array.isArray(catalog)) return findings
    for (const [agent, route] of Object.entries(routes)) {
      if (!object(route) || typeof route.model !== "string") continue
      const variant = route.variant
      if (variant === undefined || variant === "" || variant === "default") continue
      const { providerID, id: modelID } = modelRef(route.model)
      const model = catalog.find((entry) => entry?.providerID === providerID && entry?.id === modelID)
      if (!model) continue
      const available = (Array.isArray(model.variants) ? model.variants : [])
        .map((v) => (typeof v === "string" ? v : v?.id))
        .filter((id) => typeof id === "string")
      if (!available.includes(variant)) findings.push({ agent, providerID, modelID, variant, available })
    }
  } catch {}
  return findings
}

// Providers whose zero-cost models OpenCode's OPENCODE_DISABLE_FREE_MODELS hides.
const FREE_MODEL_PROVIDERS = Object.freeze(new Set(["opencode", "opencode-go"]))

/**
 * Whether a catalog model is a free OpenCode model, by the rule OpenCode applies
 * for OPENCODE_DISABLE_FREE_MODELS: an `opencode` or `opencode-go` model none of
 * whose price tiers has an input price above zero (no tiers counts as free).
 */
export function isFreeModel(model) {
  if (!object(model) || !FREE_MODEL_PROVIDERS.has(model.providerID)) return false
  const tiers = Array.isArray(model.cost) ? model.cost : []
  return !tiers.some((tier) => tier?.input > 0)
}

/** Remove free OpenCode models from a model editor; returns the removed refs. */
export function removeFreeModels(editor) {
  const removed = []
  for (const model of [...editor.list()]) {
    if (!isFreeModel(model)) continue
    editor.remove(model.providerID, model.id)
    removed.push(`${model.providerID}/${model.id}`)
  }
  return removed
}

/** Agents whose routed model is hidden as free, grouped by model. */
export function hiddenRoutedModels(routes, hidden) {
  const byModel = new Map()
  if (!object(routes) || hidden.size === 0) return byModel
  for (const [agent, route] of Object.entries(routes)) {
    if (!object(route) || !hidden.has(route.model)) continue
    byModel.set(route.model, [...(byModel.get(route.model) ?? []), agent])
  }
  return byModel
}
