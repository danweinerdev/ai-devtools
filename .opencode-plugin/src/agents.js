// The aid-* roster, read from the build's generated OpenCode agent files
// (dist/opencode/agents/*.md) and agents-manifest.json. Prompts, descriptions,
// modes and read-only are never hand-written here: tools/plugin-compiler owns
// them, so OpenCode always gets exactly what the other harnesses get.

import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"

export const ORCHESTRATOR = "aid-orchestrator"

/** The V2 permission rule a read-only agent gets: no edits anywhere. */
export const READ_ONLY_RULE = Object.freeze({ action: "edit", resource: "*", effect: "deny" })

const FRONTMATTER = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/
const BANNER = /^<!--[^\n]*-->\r?\n?/

/** `{ description, mode, body }` from one generated agent file. */
export function parseAgentFile(source) {
  const match = FRONTMATTER.exec(source)
  if (!match) throw new Error("missing frontmatter")
  const field = (key) => new RegExp(`^${key}:\\s*(.+)$`, "m").exec(match[1])?.[1]?.trim()
  const rawDescription = field("description")
  const description = rawDescription?.startsWith('"') ? JSON.parse(rawDescription) : rawDescription
  const mode = field("mode")
  if (!description || !mode) throw new Error("frontmatter needs description and mode")
  return { description, mode, body: match[2].replace(BANNER, "").trim() }
}

/**
 * Every agent to register: `[{ name, description, mode, system, readOnly }]`,
 * sorted by name. readOnly comes from agents-manifest.json (the orchestrator
 * isn't in it and isn't read-only).
 */
export function loadAgents(root) {
  const manifest = JSON.parse(readFileSync(join(root, "agents-manifest.json"), "utf8"))
  const readOnly = new Map(manifest.agents.map((agent) => [agent.name, agent.readOnly === true]))
  const dir = join(root, "dist", "opencode", "agents")
  return readdirSync(dir)
    .filter((file) => file.endsWith(".md"))
    .sort()
    .map((file) => {
      const name = file.slice(0, -3)
      let parsed
      try {
        parsed = parseAgentFile(readFileSync(join(dir, file), "utf8"))
      } catch (error) {
        throw new Error(`dist/opencode/agents/${file}: ${error.message}`)
      }
      return { name, description: parsed.description, mode: parsed.mode, system: parsed.body, readOnly: readOnly.get(name) ?? false }
    })
}

/** Write one roster agent onto a V2 agent draft (`editor.update` upserts). */
export function applyAgent(draft, agent) {
  draft.description = agent.description
  draft.mode = agent.mode
  draft.system = agent.system
  if (agent.readOnly) {
    const rules = Array.isArray(draft.permissions) ? draft.permissions : []
    const has = rules.some((r) => r?.action === READ_ONLY_RULE.action && r?.resource === READ_ONLY_RULE.resource && r?.effect === READ_ONLY_RULE.effect)
    draft.permissions = has ? rules : [...rules, { ...READ_ONLY_RULE }]
  }
}
