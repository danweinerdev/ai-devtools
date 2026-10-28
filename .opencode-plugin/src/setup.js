// The OpenCode 2 plugin: registers the aid-* roster and skills, routes models
// from aid's config, and hands every tool call to `aid guard` before it runs.
// Ported from opencode-model-router's V2 adapter. OpenCode 2 has no mutable
// global config: agents, skills and the default model go through domain
// transforms, and live operations through domain hooks with one mutable event.
//
// aid owns the rules. .opencode/aid.json is found and validated by
// `aid config`, guard decisions come from `aid guard opencode-tool`, and the
// orchestrator's routing policy from `aid guard policy`. Without aid the
// plugin still registers agents and skills, warns loudly, and lets every tool
// call through: guards fail open, agents run on the session's model.

import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

import { failure, runAid } from "./aid.js"
import { ORCHESTRATOR, applyAgent, loadAgents } from "./agents.js"
import { hiddenRoutedModels, modelRef, removeFreeModels, routesFrom, unsupportedVariants } from "./routing.js"
import { loadSkills, registerSkills } from "./skills.js"

export const PLUGIN_ID = "ai-development"
export const PACKAGE_ROOT = join(dirname(fileURLToPath(import.meta.url)), "..", "..")

const TAG = `[${PLUGIN_ID}]`
const TITLE_ROLE = "title"
const COMPACTION_ROLE = "compaction"

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

function unwrap(response) {
  return object(response) && Object.hasOwn(response, "data") ? response.data : response
}

function parseJSON(text) {
  try {
    return JSON.parse(text)
  } catch {
    return undefined
  }
}

/** The guard's verdict from aid's output, or undefined when there isn't one. */
export function guardVerdict(result) {
  const verdict = parseJSON(result.stdout)
  if (!object(verdict) || (verdict.decision !== "allow" && verdict.decision !== "deny")) return undefined
  return verdict
}

export function createSetup({ root = PACKAGE_ROOT, aid = runAid, guardTimeoutMs = 10_000, log = (line) => console.error(line) } = {}) {
  return async function setup(ctx) {
    if (typeof ctx?.agent?.transform !== "function") return

    const cwd = ctx.location?.project?.directory ?? ctx.location?.directory
    const agents = loadAgents(root)
    const roster = new Set(agents.map((agent) => agent.name))
    const skills = loadSkills(root)
    const warnings = []
    const warn = (text) => {
      warnings.push(text)
      log(`${TAG} ${text}`)
    }

    // Model routing: aid resolves and validates .opencode/aid.json.
    let config
    const configResult = await aid(["config", "--harness", "opencode", "--json"], { cwd })
    if (configResult.ok) {
      config = parseJSON(configResult.stdout)
      if (!object(config)) warn("aid config printed no JSON; agents use the session's model.")
    } else {
      warn(`model routing is off: ${failure(configResult)}. Agents use the session's model.`)
    }
    const routes = routesFrom(config)

    let policy
    const policyResult = await aid(["guard", "policy", "--harness", "opencode"], { cwd })
    if (policyResult.ok && policyResult.stdout.trim()) policy = policyResult.stdout.trim()
    else if (!policyResult.ok && !policyResult.missing) warn(`the routing policy is unavailable: ${failure(policyResult)}`)

    let strippedVariants = new Set()
    let variantWarnings = []
    let appliedFindingsKey = "[]"
    const modelFor = (name) => {
      const route = routes[name]
      if (!route) return undefined
      return modelRef(route.model, strippedVariants.has(name) ? undefined : route.variant)
    }

    await ctx.agent.transform((editor) => {
      for (const agent of agents) {
        editor.update(agent.name, (draft) => {
          applyAgent(draft, agent)
          const model = modelFor(agent.name)
          if (model) draft.model = model
        })
      }
      // Agents routed by name that this plugin doesn't define: an existing one
      // only gets its model; a missing one becomes a subagent stub, so it can
      // never surface as an unrestricted primary agent.
      for (const name of Object.keys(routes)) {
        if (roster.has(name)) continue
        const exists = editor.get(name) !== undefined
        editor.update(name, (draft) => {
          if (!exists) draft.mode = "subagent"
          draft.model = modelFor(name)
        })
      }
      editor.default(ORCHESTRATOR)
    })

    // Registered before the default-model transform, so a free default model is
    // never selected. Every catalog rebuild replays it.
    let hiddenFreeModels = new Set()
    if (config?.hide_free_models === true) {
      let reported = false
      await ctx.model.transform((editor) => {
        try {
          hiddenFreeModels = new Set(removeFreeModels(editor))
        } catch (error) {
          if (reported) return
          reported = true
          log(`${TAG} hide_free_models could not filter the model catalog: ${error?.message ?? error}`)
        }
      })
    }

    // OpenCode runs a primary turn on the session's stored model, or the
    // default model, never on the agent's own model: routing the orchestrator
    // means making its model the default.
    const orchestratorRoute = routes[ORCHESTRATOR]
    if (orchestratorRoute) {
      const { providerID, id } = modelRef(orchestratorRoute.model)
      await ctx.model.transform((editor) => {
        if (editor.get(providerID, id) === undefined) return
        editor.default.set(providerID, id)
      })
    }

    await ctx.skill.transform((editor) => registerSkills(editor, skills))

    const runRevalidation = async () => {
      try {
        const catalog = unwrap(await ctx.model.list())
        if (!Array.isArray(catalog)) return
        const findings = unsupportedVariants(routes, catalog).sort((a, b) => (a.agent < b.agent ? -1 : a.agent > b.agent ? 1 : 0))
        const key = JSON.stringify(findings)
        if (key === appliedFindingsKey) return
        const previous = strippedVariants
        strippedVariants = new Set(findings.map((finding) => finding.agent))
        try {
          await ctx.agent.reload()
        } catch {
          strippedVariants = previous
          return
        }
        appliedFindingsKey = key
        variantWarnings = findings.map(({ agent, providerID, modelID, variant, available }) =>
          `variant ${JSON.stringify(variant)} is not available for ${providerID}/${modelID} (agent ${agent}); it was ignored. Available: ${available.length ? available.join(", ") : "none"}.`,
        )
        for (const text of variantWarnings) log(`${TAG} ${text}`)
      } catch {}
    }
    let revalidationChain = Promise.resolve()
    const revalidateVariants = () => (revalidationChain = revalidationChain.then(runRevalidation, runRevalidation))

    // Routed agents another plugin or agent file was meant to define, resolved
    // against the final registry the first time the orchestrator needs it.
    let unresolved
    const resolveUnresolved = async () => {
      const external = Object.keys(routes).filter((name) => !roster.has(name))
      if (unresolved || external.length === 0) return unresolved ?? []
      try {
        const list = unwrap(await ctx.agent.list())
        const defined = new Set((Array.isArray(list) ? list : []).filter((a) => typeof a?.system === "string").map((a) => a.id))
        unresolved = external.filter((name) => !defined.has(name))
      } catch {
        return []
      }
      return unresolved
    }

    const applyReasoningEffort = (event, agent) => {
      const effort = routes[agent]?.reasoningEffort
      if (effort !== undefined && object(event.options) && event.options.reasoningEffort === undefined) event.options.reasoningEffort = effort
    }

    await ctx.session.hook("context", async (event) => {
      applyReasoningEffort(event, event.agent)
      if (event.agent !== ORCHESTRATOR) return
      const notes = []
      if (policy) notes.push(policy)
      for (const text of warnings) notes.push(`ai-development configuration warning: ${text}`)
      const missing = await resolveUnresolved()
      if (missing.length) {
        notes.push(`ai-development configuration warning: aid.json routes agent(s) ${missing.join(", ")} but nothing defines them; they have a model but no prompt.`)
      }
      for (const [model, names] of hiddenRoutedModels(routes, hiddenFreeModels)) {
        notes.push(`ai-development configuration warning: ${model} is a free OpenCode model hidden by hide_free_models, so agent(s) ${names.join(", ")} cannot run on it.`)
      }
      for (const text of variantWarnings) notes.push(`ai-development configuration warning: ${text}`)
      for (const text of notes) event.system.push({ type: "text", text })
    })
    await ctx.session.hook("generate", (event) => applyReasoningEffort(event, event.agent))
    await ctx.session.hook("compaction", (event) => applyReasoningEffort(event, COMPACTION_ROLE))
    await ctx.session.hook("title", (event) => applyReasoningEffort(event, TITLE_ROLE))

    // Every tool call goes to aid's guard first. A deny blocks the call with
    // aid's reason; aid missing, failing or timing out lets it through (the
    // guards fail open) and is reported once.
    let reportedGuardFailure = false
    await ctx.tool.hook("execute.before", async (event) => {
      const input = JSON.stringify({
        tool: event.tool,
        args: object(event.input) ? event.input : {},
        sessionID: event.sessionID,
        agent: event.agent,
        cwd,
      })
      const result = await aid(["guard", "opencode-tool"], { input, cwd, timeoutMs: guardTimeoutMs })
      const verdict = guardVerdict(result)
      if (verdict?.decision === "deny") throw new Error(verdict.reason || `aid guard denied ${event.tool}`)
      if (verdict === undefined && !reportedGuardFailure) {
        reportedGuardFailure = true
        log(`${TAG} guards are off until aid answers: ${result.ok ? "aid guard printed no verdict" : failure(result)}`)
      }
    })

    const controller = new AbortController()
    void (async () => {
      try {
        for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
          if (event?.type === "model.updated") await revalidateVariants()
        }
      } catch {}
    })()

    await revalidateVariants()

    return async () => {
      controller.abort()
    }
  }
}

export const setup = createSetup()
