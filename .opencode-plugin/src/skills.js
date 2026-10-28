// Registers every aid-* skill from skills-manifest.json in place: the skill's
// path points back into this package, so nothing is copied into the user's
// skill directories. Verified on OpenCode 2.2.0 (all 21 skills listed).

import { readFileSync } from "node:fs"
import { join } from "node:path"

const body = (source) => source.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, "")

/** `[{ id, name, description, path, content }]` for every skill in the manifest. */
export function loadSkills(root) {
  const manifest = JSON.parse(readFileSync(join(root, "skills-manifest.json"), "utf8"))
  return manifest.skills.map((skill) => {
    const path = join(root, skill.path)
    return { id: skill.name, name: skill.name, description: skill.description, path, content: body(readFileSync(path, "utf8")) }
  })
}

/** Add each skill, replacing an older registration of the same id. */
export function registerSkills(editor, skills) {
  for (const skill of skills) {
    if (editor.get(skill.id) !== undefined) editor.remove(skill.id)
    editor.add(skill)
  }
}
