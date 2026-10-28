// The bridge to the `aid` binary. Model-routing config and guard decisions are
// aid's: this module only runs it and reports what happened. aid is spawned
// directly (no shell), resolved from PATH, with a timeout, and never throws:
// callers decide what a missing or failing aid means.

import { spawn } from "node:child_process"

export const DEFAULT_TIMEOUT_MS = 10_000

/**
 * Run aid with `args`, writing `input` (if any) to its stdin. Resolves
 * `{ ok, code, stdout, stderr, missing, timedOut }`: `ok` only for exit 0,
 * `missing` when aid isn't on PATH. `command` is `[executable, ...prefixArgs]`,
 * so tests can run a stub script through node.
 */
export function runAid(args, { input, timeoutMs = DEFAULT_TIMEOUT_MS, command = ["aid"], cwd, env } = {}) {
  return new Promise((resolve) => {
    const [exe, ...prefix] = command
    let child
    try {
      child = spawn(exe, [...prefix, ...args], { cwd, env, stdio: ["pipe", "pipe", "pipe"], windowsHide: true })
    } catch (error) {
      resolve({ ok: false, code: -1, stdout: "", stderr: String(error?.message ?? error), missing: error?.code === "ENOENT", timedOut: false })
      return
    }
    let stdout = ""
    let stderr = ""
    let settled = false
    const finish = (result) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      resolve(result)
    }
    const timer = setTimeout(() => {
      child.kill("SIGKILL")
      finish({ ok: false, code: -1, stdout, stderr, missing: false, timedOut: true })
    }, timeoutMs)
    child.stdout.setEncoding("utf8").on("data", (chunk) => (stdout += chunk))
    child.stderr.setEncoding("utf8").on("data", (chunk) => (stderr += chunk))
    child.on("error", (error) =>
      finish({ ok: false, code: -1, stdout, stderr: stderr || String(error?.message ?? error), missing: error?.code === "ENOENT", timedOut: false }),
    )
    child.on("close", (code) => finish({ ok: code === 0, code: code ?? -1, stdout, stderr, missing: false, timedOut: false }))
    // A child that exits without reading stdin makes the write fail with EPIPE.
    child.stdin.on("error", () => {})
    child.stdin.end(input === undefined ? "" : input)
  })
}

/** One line describing why an aid call didn't succeed, for logs and warnings. */
export function failure(result) {
  if (result.missing) return "aid is not on PATH"
  if (result.timedOut) return "aid timed out"
  const detail = (result.stderr || result.stdout || "").trim().replace(/\s+/g, " ")
  return `aid exited ${result.code}${detail ? `: ${detail}` : ""}`
}
