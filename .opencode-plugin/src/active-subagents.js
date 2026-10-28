// Selects the conversation's running sub-agents and their models for the TUI
// sidebar. Plain JavaScript so it is unit-tested without the TUI.
//
// It never throws: it runs inside a Solid memo, where an uncaught error could
// take down the sidebar. But a failure must not look like "nothing running",
// so errors are returned alongside the rows for the panel to display instead of
// being swallowed into an empty list.

const MAX_ERROR_LENGTH = 120

export function formatModel(model) {
  try {
    const providerID = model?.providerID
    const id = model?.id
    if (typeof providerID !== "string" || !providerID || typeof id !== "string" || !id) {
      return "model unknown"
    }
    const variant = model.variant
    const suffix = typeof variant === "string" && variant && variant !== "default" ? ` [${variant}]` : ""
    return `${providerID}/${id}${suffix}`
  } catch {
    return "model unknown"
  }
}

function describe(error) {
  let text
  try {
    text = typeof error?.message === "string" && error.message ? error.message : String(error)
  } catch {
    text = "unknown error"
  }
  text = text.replace(/\s+/g, " ").trim() || "unknown error"
  return text.length > MAX_ERROR_LENGTH ? `${text.slice(0, MAX_ERROR_LENGTH - 1)}…` : text
}

/**
 * `{ rows, error }`. `error` is the first failure met, or `undefined`.
 *
 * A failure resolving the conversation (`root`/`family`) yields no rows. A
 * failure reading one session affects only that session: if its status cannot
 * be read it is left out, and if its info cannot be read it is still listed
 * with unknown labels. Either way `error` is set, so the panel can say the list
 * is incomplete rather than silently dropping it.
 */
export function readActiveSubagents(sessionID, data) {
  let error
  const note = (cause) => {
    if (error === undefined) error = describe(cause)
  }

  let rootID
  let family
  try {
    rootID = data.root(sessionID)
    family = data.family(sessionID)
  } catch (cause) {
    note(cause)
    return { rows: [], error }
  }
  if (typeof rootID !== "string" || !rootID) return { rows: [], error: "conversation root is unavailable" }
  if (!Array.isArray(family)) return { rows: [], error: "conversation sessions are unavailable" }

  const running = family.filter((id) => {
    // Root first: its status is never read, so the main agent starting and
    // stopping does not re-run the memo that calls this.
    if (typeof id !== "string" || !id || id === rootID) return false
    try {
      return data.status(id) === "running"
    } catch (cause) {
      note(cause)
      return false
    }
  })

  const rows = running
    .map((id) => {
      try {
        const info = data.get(id)
        const agent = info?.agent
        const created = info?.time?.created
        return {
          sessionID: id,
          agent: typeof agent === "string" && agent ? agent : "unknown agent",
          model: formatModel(info?.model),
          created: typeof created === "number" && Number.isFinite(created) ? created : 0,
        }
      } catch (cause) {
        note(cause)
        return { sessionID: id, agent: "unknown agent", model: "model unknown", created: 0 }
      }
    })
    .sort((a, b) => a.created - b.created || (a.sessionID < b.sessionID ? -1 : a.sessionID > b.sessionID ? 1 : 0))
    .map(({ sessionID: id, agent, model }) => ({ sessionID: id, agent, model }))

  return { rows, error }
}

/**
 * `next` rows, reusing each `previous` row object whose session, agent and
 * model are unchanged, and returning `previous` itself when nothing changed.
 *
 * Solid's `<For>` matches rows by object identity. Rebuilding every object on
 * each recomputation would tear down and recreate every row even when nothing
 * visible changed; reusing them means only rows that started or finished move.
 */
export function reuseRows(previous, next) {
  if (!Array.isArray(next)) return Array.isArray(previous) && previous.length === 0 ? previous : []
  if (!Array.isArray(previous)) return next
  // Empty to empty is the common idle case; keep it from notifying anything.
  if (previous.length === 0) return next.length === 0 ? previous : next
  const bySession = new Map(previous.map((row) => [row?.sessionID, row]))
  const rows = next.map((row) => {
    const old = bySession.get(row.sessionID)
    return old && old.agent === row.agent && old.model === row.model ? old : row
  })
  return rows.length === previous.length && rows.every((row, index) => row === previous[index]) ? previous : rows
}

/**
 * `next` as a `{ rows, error }` result, reusing `previous` and its row objects
 * where unchanged. Returning the same object lets a Solid memo skip notifying
 * anything downstream when a recomputation found no visible change.
 */
export function reuseResult(previous, next) {
  const rows = reuseRows(previous?.rows, next?.rows)
  const error = next?.error
  return previous && rows === previous.rows && error === previous.error ? previous : { rows, error }
}

/** The rows alone; failures are visible only through `readActiveSubagents`. */
export function activeSubagents(sessionID, data) {
  return readActiveSubagents(sessionID, data).rows
}
