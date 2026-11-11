// Keeps a subagent session on the agent it was dispatched as.
//
// OpenCode 2 lets a `subagent` call pass an existing child `sessionID` with a
// different `agent`: it switches the child to that agent and its model and
// carries the whole conversation across. An implementer's context would leak
// into a fresh-context review lens, or a read-only explorer's session would be
// reused as a writer. This guard remembers each child's original agent and
// rejects a continuation under any other one. Continuing as the same agent is
// always allowed.
//
// It never blocks on doubt: when the original agent can't be established (no
// dispatch seen, history compacted, a lookup that fails or times out), the
// call goes through.

/** OpenCode 2's subagent tool: `{agent, prompt, description, sessionID?, background?, model?}`. */
export const SUBAGENT_TOOL = "subagent"
export const DEFAULT_HISTORY_TIMEOUT_MS = 3_000

function object(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

function id(value) {
  return typeof value === "string" && value ? value : undefined
}

/**
 * The agent a session was first run as, from its chronological context
 * history: the first assistant message's agent, or the agent an agent switch
 * moved away from (or, with no previous agent, the one it set). A compaction
 * met first means the start of the history is gone, so the answer is unknown.
 */
export function originalAgentFromHistory(messages) {
  if (!Array.isArray(messages)) return undefined
  for (const message of messages) {
    if (!object(message)) continue
    if (message.type === "compaction") return undefined
    if (message.type === "agent-switched") return id(message.previous) ?? id(message.agent)
    if (message.type === "assistant") return id(message.agent)
  }
  return undefined
}

/** The session a `session.deleted` event names, in OpenCode 2's or the older event shape. */
export function deletedSessionID(event) {
  if (event?.type !== "session.deleted") return undefined
  const data = event.data ?? event.properties
  return id(data?.sessionID) ?? id(data?.info?.id)
}

export function blockedMessage(sessionID, original, requested) {
  return (
    `Subagent session ${sessionID} was dispatched as ${original}; it can't be continued as ${requested}, ` +
    `because that would carry ${original}'s conversation into ${requested}. ` +
    `Continue it as ${original}, or start a new ${requested} session (omit sessionID) and give it the relevant findings.`
  )
}

/**
 * `history(sessionID)` resolves the session's context messages (OpenCode's
 * `ctx.session.context`). Entries are first evidence wins: a later dispatch
 * result never redefines a session's role.
 */
export function createContinuationGuard({ history, timeoutMs = DEFAULT_HISTORY_TIMEOUT_MS } = {}) {
  const dispatched = new Map()

  const recover = async (sessionID) => {
    if (typeof history !== "function") return undefined
    let timer
    try {
      const timeout = new Promise((resolve) => {
        timer = setTimeout(resolve, timeoutMs, undefined)
      })
      const response = await Promise.race([Promise.resolve().then(() => history(sessionID)), timeout])
      const messages = object(response) && Object.hasOwn(response, "data") ? response.data : response
      return originalAgentFromHistory(messages)
    } catch {
      return undefined
    } finally {
      clearTimeout(timer)
    }
  }

  return {
    /** The `execute.before` half: throws when a continuation would switch the child's agent. */
    async check(event) {
      if (event?.tool !== SUBAGENT_TOOL || !object(event.input)) return
      const sessionID = id(event.input.sessionID)
      const requested = id(event.input.agent)
      if (!sessionID || !requested) return
      let original = dispatched.get(sessionID)
      if (original === undefined) {
        original = await recover(sessionID)
        if (original === undefined) return
        if (!dispatched.has(sessionID)) dispatched.set(sessionID, original)
        original = dispatched.get(sessionID)
      }
      if (original !== requested) throw new Error(blockedMessage(sessionID, original, requested))
    },

    /**
     * The `execute.after` half: a new dispatch's result names the child it
     * created, foreground or background (`metadata.sessionID`). Continuations
     * are not recorded, since the agent they ran as is not proof of the
     * original.
     */
    record(event) {
      if (event?.tool !== SUBAGENT_TOOL || event.status !== "completed" || !object(event.input)) return
      if (id(event.input.sessionID)) return
      const child = id(event.result?.metadata?.sessionID)
      const agent = id(event.input.agent)
      if (child && agent && !dispatched.has(child)) dispatched.set(child, agent)
    },

    forget(sessionID) {
      if (sessionID) dispatched.delete(sessionID)
    },

    clear() {
      dispatched.clear()
    },

    /** For tests: the remembered agent, if any. */
    known(sessionID) {
      return dispatched.get(sessionID)
    },
  }
}
