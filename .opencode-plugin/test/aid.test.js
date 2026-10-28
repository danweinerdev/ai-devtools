// runAid against a real child process: a node script standing in for aid, so
// it runs the same on every OS.
import assert from "node:assert/strict"
import { mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import test from "node:test"

import { failure, runAid } from "../src/aid.js"

const dir = mkdtempSync(join(tmpdir(), "aid-stub-"))
const stub = join(dir, "aid.mjs")
writeFileSync(
  stub,
  `let input = ""
process.stdin.on("data", (c) => (input += c))
process.stdin.on("end", () => {
  const args = process.argv.slice(2)
  if (args[0] === "sleep") setTimeout(() => {}, 60_000)
  else if (args[0] === "fail") { process.stderr.write("bad config"); process.exit(1) }
  else process.stdout.write(JSON.stringify({ args, input }))
})
`,
)
const command = [process.execPath, stub]

test("runAid passes args and stdin and returns stdout", async () => {
  const result = await runAid(["guard", "opencode-tool"], { command, input: '{"tool":"shell"}' })
  assert.equal(result.ok, true)
  assert.deepEqual(JSON.parse(result.stdout), { args: ["guard", "opencode-tool"], input: '{"tool":"shell"}' })
})

test("a non-zero exit is reported with stderr", async () => {
  const result = await runAid(["fail"], { command })
  assert.equal(result.ok, false)
  assert.equal(result.code, 1)
  assert.equal(failure(result), "aid exited 1: bad config")
})

test("a missing aid is reported as missing, never thrown", async () => {
  const result = await runAid(["config"], { command: ["aid-definitely-not-installed-xyz"] })
  assert.equal(result.missing, true)
  assert.equal(failure(result), "aid is not on PATH")
})

test("a hung aid is killed at the timeout", async () => {
  const started = Date.now()
  const result = await runAid(["sleep"], { command, timeoutMs: 300 })
  assert.equal(result.timedOut, true)
  assert.ok(Date.now() - started < 5_000)
  assert.equal(failure(result), "aid timed out")
})
