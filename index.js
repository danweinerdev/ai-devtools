// OpenCode V2 plugin entry. Registers this package's skills in place, so
// nothing is copied into the user's skill directories.
//
// Agents are NOT registered here: the V2 agent editor can update and remove
// agents but has no add. Agents install as files from dist/opencode/agents/
// (see `make install-opencode`).
//
// UNVERIFIED against a live OpenCode 2.x session; written from the V2 plugin
// docs (Plugin.define shape, ctx.skill.transform editor.add).

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = dirname(fileURLToPath(import.meta.url));
const manifest = JSON.parse(readFileSync(join(ROOT, "skills-manifest.json"), "utf8"));

const body = (file) => readFileSync(file, "utf8").replace(/^---\n[\s\S]*?\n---\n?/, "");

export default {
  id: manifest.plugin,
  async setup(ctx) {
    await ctx.skill.transform((editor) => {
      for (const skill of manifest.skills) {
        const path = join(ROOT, skill.path);
        if (editor.get(skill.name)) editor.remove(skill.name);
        editor.add({
          id: skill.name,
          name: skill.name,
          description: skill.description,
          path,
          content: body(path),
        });
      }
    });
  },
};
