// A fake OpenCode 2 plugin context with the editor semantics the plugin relies
// on: agent `update` upserts (as OpenCode 2.2.0 does), skill `add`/`remove`,
// model `default.set`, and hook registries tests can fire.

export function fakeContext({ catalog = [], existingAgents = {}, location = { directory: "/project" } } = {}) {
  const agents = new Map(Object.entries(existingAgents).map(([id, value]) => [id, { id, permissions: [], ...value }]))
  const skills = new Map()
  const models = new Map(catalog.map((model) => [`${model.providerID}/${model.id}`, model]))
  const hooks = { session: {}, tool: {} }
  const state = { defaultAgent: undefined, defaultModel: undefined, reloads: 0, agentTransforms: [] }
  const events = []
  let push

  const agentEditor = {
    list: () => [...agents.values()],
    get: (id) => agents.get(id),
    update: (id, fn) => {
      const agent = agents.get(id) ?? { id, mode: "primary", permissions: [] }
      if (!agents.has(id)) agents.set(id, agent)
      fn(agent)
      agent.id = id
    },
    remove: (id) => agents.delete(id),
    default: (id) => (state.defaultAgent = id),
  }

  const ctx = {
    location,
    agent: {
      transform: async (fn) => {
        state.agentTransforms.push(fn)
        fn(agentEditor)
      },
      // A reload replays every transform, as OpenCode does.
      reload: async () => {
        state.reloads += 1
        for (const fn of state.agentTransforms) fn(agentEditor)
      },
      list: async () => [...agents.values()],
    },
    skill: {
      transform: async (fn) =>
        fn({
          list: () => [...skills.values()],
          get: (id) => skills.get(id),
          add: (skill) => skills.set(skill.id, skill),
          update: (id, fn2) => fn2(skills.get(id)),
          remove: (id) => skills.delete(id),
        }),
    },
    model: {
      list: async () => [...models.values()],
      transform: async (fn) =>
        fn({
          list: () => [...models.values()],
          get: (providerID, id) => models.get(`${providerID}/${id}`),
          remove: (providerID, id) => models.delete(`${providerID}/${id}`),
          default: { set: (providerID, id) => (state.defaultModel = `${providerID}/${id}`) },
        }),
    },
    session: { hook: async (name, fn) => (hooks.session[name] = fn) },
    tool: { hook: async (name, fn) => (hooks.tool[name] = fn) },
    event: {
      subscribe: ({ signal } = {}) => ({
        async *[Symbol.asyncIterator]() {
          while (!signal?.aborted) {
            if (events.length) {
              yield events.shift()
              continue
            }
            await new Promise((resolve) => {
              push = resolve
              signal?.addEventListener("abort", resolve, { once: true })
            })
          }
        },
      }),
    },
  }

  return {
    ctx,
    agents,
    skills,
    models,
    hooks,
    state,
    emit: (event) => {
      events.push(event)
      push?.()
    },
  }
}

/** An `aid` stand-in: `responses` maps a joined argv to a result or a function of the call. */
export function fakeAid(responses) {
  const calls = []
  const aid = async (args, options = {}) => {
    calls.push({ args, options })
    const handler = responses[args.join(" ")]
    const result = typeof handler === "function" ? handler(options) : handler
    return { ok: false, code: -1, stdout: "", stderr: "", missing: true, timedOut: false, ...(result ?? {}) }
  }
  return { aid, calls }
}

export const ok = (stdout) => ({ ok: true, code: 0, stdout, stderr: "", missing: false })
