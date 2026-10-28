// Live OpenCode 2 e2e suite for the ai-development plugin, ported from
// opencode-model-router's live suite. It proves, against a real OpenCode 2
// server and real models, that:
//   - the plugin registers all seven aid-* agents (modes) and every aid-* skill;
//   - each aid-* subagent the user's aid.json maps to a profile is registered
//     with, and actually dispatched on, that profile's model (registry, child
//     session and assistant messages must agree; an unsupported variant must
//     be stripped);
//   - a routed aid-orchestrator's model is what primary turns run on;
//   - the guard bridge blocks a dispatch aid's guard denies (a sensitivity
//     rule planted in the scratch copy of aid.json).
// A private loopback server uses the normal OpenCode provider database; only
// this tool's server and probe sessions are cleaned up. Server output is never
// echoed. This makes real model calls and can incur provider costs.
//
//   node .opencode-plugin/live/e2e.js --config PATH [--keep] [--opencode PATH] [--agents all|ID,ID,...]
//   (or set AID_E2E_CONFIG=PATH)
//
// Exit status: 0 all PASS, 1 any FAIL, 2 UNVERIFIED, skipped or could not run.

import { execFile, execFileSync, spawn } from "node:child_process"
import { readFileSync, realpathSync } from "node:fs"
import { mkdir, mkdtemp, rm, stat, writeFile } from "node:fs/promises"
import { createServer } from "node:net"
import { tmpdir } from "node:os"
import { delimiter, dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

import { compareModels, inspectSubagentCalls, modelTuple, primarySessionEvidence, statusMax, summarize } from "./verify-models.js"

const REPO = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..")
const PLUGIN_ID = "ai-development"
const ORCHESTRATOR = "aid-orchestrator"
const SUBAGENTS = ["aid-craftsman", "aid-creator", "aid-designer", "aid-explorer", "aid-mechanic", "aid-oracle"]
const GUARD_ROW = "guard"
const SENTINEL = "aid-e2e-sentinel"
const SECRET = "AID-E2E-SECRET-4242"
const STARTUP_TIMEOUT_MS = 60_000
const RUN_TIMEOUT_MS = 180_000
const HTTP_TIMEOUT_MS = 10_000
const MAX_API_BODY = 1024 * 1024
const USAGE = "Usage: node .opencode-plugin/live/e2e.js [--config PATH] [--keep] [--opencode PATH] [--agents all|ID,ID,...]  (or AID_E2E_CONFIG=PATH; default: the aid.json aid resolves, as the plugin does)"
const timer = (ms, callback) => { const value = setTimeout(callback, ms); value.unref?.(); return value }
const stableError = (code) => { const error = new Error(code); error.code = code; return error }
const unwrap = (value) => value && typeof value === "object" && "data" in value ? value.data : value

function sleep(ms, signal) {
  return new Promise((accept, reject) => {
    const done = () => { signal?.removeEventListener("abort", abort); accept() }
    const abort = () => { clearTimeout(timeoutID); signal?.removeEventListener("abort", abort); reject(stableError("INTERRUPTED")) }
    const timeoutID = setTimeout(done, ms)
    signal?.addEventListener("abort", abort, { once: true })
    if (signal?.aborted) abort()
  })
}

export function parseArgs(argv, env = process.env) {
  const options = { keep: false, opencode: "opencode", agents: "all", config: env.AID_E2E_CONFIG || undefined }
  const seen = new Set()
  for (let i = 0; i < argv.length; i++) {
    const token = argv[i]
    if (!/^--[a-z-]+$/.test(token)) throw new Error(`Malformed option: ${JSON.stringify(token)}`)
    if (!["--keep", "--opencode", "--agents", "--config"].includes(token)) throw new Error(`Unknown option: ${token}`)
    if (seen.has(token)) throw new Error(`Duplicate option: ${token}`)
    seen.add(token)
    if (token === "--keep") { options.keep = true; continue }
    const value = argv[++i]
    if (!value || !value.trim() || value.startsWith("-")) throw new Error(`Missing value for ${token}`)
    if (token === "--opencode") options.opencode = value
    else if (token === "--config") options.config = value
    else {
      const names = value === "all" ? "all" : value.split(",")
      if (names !== "all" && (names.some((name) => !name || name !== name.trim()) || new Set(names).size !== names.length)) {
        throw new Error("--agents requires distinct, nonempty agent IDs or all")
      }
      const known = [ORCHESTRATOR, ...SUBAGENTS, GUARD_ROW]
      const unknown = names === "all" ? [] : names.filter((name) => !known.includes(name))
      if (unknown.length) throw new Error(`Unknown row(s): ${unknown.join(", ")}; known: ${known.join(", ")}`)
      options.agents = names
    }
  }
  return options
}

function splitModel(spec) {
  const hash = spec.indexOf("#")
  const model = hash === -1 ? spec : spec.slice(0, hash)
  const variant = hash === -1 ? undefined : spec.slice(hash + 1) || undefined
  const slash = model.indexOf("/")
  return { model, providerID: model.slice(0, slash), modelID: model.slice(slash + 1), variant }
}

/**
 * The rows to run, from aid's resolved config: every aid-* subagent mapped to
 * a profile (with its profile's model), the orchestrator if mapped, and the
 * guard row. Unmapped subagents inherit the session model, so there's nothing
 * routed to prove for them: they're reported as skipped.
 */
export function planRows(resolved, selection = "all") {
  const profiles = resolved?.profiles ?? {}
  const agents = resolved?.agents ?? {}
  const rows = []
  const skipped = []
  for (const agent of [ORCHESTRATOR, ...SUBAGENTS]) {
    const profile = profiles[agents[agent]]
    if (typeof profile?.model !== "string" || !profile.model.includes("/")) { skipped.push(agent); continue }
    const { model, providerID, modelID, variant } = splitModel(profile.model)
    rows.push({ agent, mode: agent === ORCHESTRATOR ? "primary" : "subagent", model, variant, providerID, modelID })
  }
  rows.push({ agent: GUARD_ROW, mode: "guard" })
  const chosen = selection === "all" ? rows : rows.filter((row) => selection.includes(row.agent))
  return { rows: chosen, skipped: selection === "all" ? skipped : skipped.filter((agent) => selection.includes(agent)) }
}

/**
 * A scratch copy of the user's aid.json: the sentinel (an external agent with
 * no model of its own) routed to the first subagent row's profile, proving the
 * scratch config is the one loaded, and a sensitivity rule the guard row trips.
 */
export function scratchConfig(user, rows) {
  const config = structuredClone(user)
  const first = rows.find((row) => row.mode === "subagent")
  config.agents = { ...(config.agents ?? {}) }
  if (first) config.agents[SENTINEL] = config.agents[first.agent]
  const rule = { name: "aid-e2e", patterns: [SECRET], allow_agents: [] }
  config.sensitive = { rules: [...(config.sensitive?.rules ?? []), rule] }
  return config
}

// npm can expose OpenCode through a shell/.cmd shim on Windows. Execute the
// native V2 package binary directly; never enable shell interpretation.
async function resolveOpenCode(name) {
  if (process.platform !== "win32" || name !== "opencode") return name
  const directories = (process.env.PATH ?? "").split(delimiter).filter(Boolean)
  if (process.env.APPDATA) directories.push(join(process.env.APPDATA, "npm"))
  for (const entry of directories) {
    const directory = entry.replace(/^"(.*)"$/, "$1")
    for (const candidate of [join(directory, "opencode.exe"), join(directory, "node_modules", "@opencode", "cli", "bin", "opencode.exe")]) {
      try { if ((await stat(candidate)).isFile()) return candidate } catch {}
    }
  }
  throw stableError("OPENCODE_EXECUTABLE_NOT_FOUND")
}

function runQuiet(binary, args, options = {}) {
  return new Promise((accept) => {
    execFile(binary, args, { windowsHide: true, timeout: 30_000, ...options }, (error, stdout, stderr) =>
      accept({ ok: !error, stdout: String(stdout ?? ""), stderr: String(stderr ?? ""), missing: error?.code === "ENOENT" }))
  })
}

/**
 * The config the plugin itself would use: aid resolves the nearest
 * .opencode/aid.json, else ~/.config/opencode/aid.json, and validates it.
 * Returns { path } or a skip naming why there is none.
 */
export async function defaultConfig(run = runQuiet, cwd = process.cwd()) {
  const resolved = await run("aid", ["config", "--harness", "opencode", "--json"], { cwd })
  if (resolved.missing) return { skip: "AID_NOT_ON_PATH", hint: "make install-aid (the plugin's routing and guards run through aid)" }
  if (!resolved.ok) return { skip: "AID_CONFIG_INVALID", hint: resolved.stderr.trim() || "aid config failed" }
  let source
  try { ({ source } = JSON.parse(resolved.stdout)) } catch { return { skip: "AID_CONFIG_UNREADABLE", hint: "aid config printed no JSON" } }
  if (!source) return { skip: "AID_CONFIG_NOT_FOUND", hint: "no .opencode/aid.json here or above and no ~/.config/opencode/aid.json; create one or pass --config" }
  return { path: source }
}

/** Prerequisites, each a stable code: a skip names what's missing and how to fix it. */
async function prerequisites(options) {
  if (!options.config) {
    const found = await defaultConfig()
    if (found.skip) return found
    options.config = found.path
  }
  console.log(`config: ${options.config}`)
  let user
  try { user = JSON.parse(readFileSync(options.config, "utf8")) } catch { return { skip: "AID_E2E_CONFIG_UNREADABLE", hint: `${options.config} isn't readable JSON` } }
  const binary = await resolveOpenCode(options.opencode)
  const version = await runQuiet(binary, ["--version"])
  if (version.missing) return { skip: "OPENCODE_NOT_FOUND", hint: "install OpenCode 2 or pass --opencode PATH" }
  if (!/\b2\.\d+/.test(version.stdout + version.stderr)) return { skip: "OPENCODE_NOT_V2", hint: "the suite targets OpenCode 2 only" }
  const aid = await runQuiet("aid", ["guard", "--help"])
  if (aid.missing) return { skip: "AID_NOT_ON_PATH", hint: "make install-aid (the plugin's routing and guards run through aid)" }
  const git = await runQuiet("git", ["--version"])
  if (git.missing) return { skip: "GIT_NOT_FOUND", hint: "install git" }
  return { binary, user }
}

async function freePort() {
  return new Promise((accept, reject) => {
    const server = createServer().listen(0, "127.0.0.1", () => {
      const { port } = server.address()
      server.close(() => accept(port))
    })
    server.on("error", reject)
  })
}

async function writeScratch(root, config) {
  await mkdir(join(root, ".opencode", "agents"), { recursive: true })
  await writeFile(join(root, ".opencode", "aid.json"), `${JSON.stringify(config, null, 2)}\n`)
  await writeFile(join(root, "opencode.json"), `${JSON.stringify({ plugins: [join(REPO, "index.js")] }, null, 2)}\n`)
  // Defined by the scratch project, not by this repo, with no model of its own.
  await writeFile(join(root, ".opencode", "agents", `${SENTINEL}.md`), [
    "---", "description: Live routing probe defined outside the ai-development repo.",
    "mode: subagent", "permissions:", '  - action: "*"', '    resource: "*"', "    effect: deny",
    "---", "", "Reply with exactly the single word PONG. Do not call any other tool.", "",
  ].join("\n"))
  execFileSync("git", ["init", "-q"], { cwd: root, windowsHide: true, timeout: 30_000, stdio: "ignore" })
}

// `opencode serve` prints a per-process password. Never print it or HTTP bodies.
const authorization = (password) => `Basic ${Buffer.from(`opencode:${password}`).toString("base64")}`

async function boundedFetch(url, options, timeout = HTTP_TIMEOUT_MS, outerSignal) {
  const controller = new AbortController()
  const abort = () => controller.abort()
  outerSignal?.addEventListener("abort", abort, { once: true })
  if (outerSignal?.aborted) abort()
  const timeoutID = timer(timeout, abort)
  try {
    const response = await fetch(url, { ...options, signal: controller.signal })
    if (!response?.ok) throw stableError(`HTTP_${response?.status ?? "ERROR"}`)
    if (response.status === 204) return null
    const length = Number(response.headers?.get?.("content-length"))
    if (Number.isFinite(length) && length > MAX_API_BODY) throw stableError("HTTP_BODY_TOO_LARGE")
    const text = await response.text()
    if (Buffer.byteLength(text) > MAX_API_BODY) throw stableError("HTTP_BODY_TOO_LARGE")
    if (!text) return undefined
    try { return JSON.parse(text) } catch { throw stableError("HTTP_BODY_INVALID") }
  } catch (error) {
    if (outerSignal?.aborted) throw stableError("INTERRUPTED")
    if (controller.signal.aborted) throw stableError("HTTP_TIMEOUT")
    if (error?.code) throw error
    throw stableError("HTTP_FAILED")
  } finally {
    clearTimeout(timeoutID)
    outerSignal?.removeEventListener("abort", abort)
  }
}

function client(base, directory, password, runSignal) {
  const request = async (method, path, body, timeout, signal = runSignal) => {
    const url = new URL(path, base)
    if (method === "GET") url.searchParams.set("location[directory]", directory)
    return unwrap(await boundedFetch(url, {
      method,
      headers: { authorization: authorization(password), ...(body ? { "content-type": "application/json" } : {}) },
      body: body ? JSON.stringify(body) : undefined,
    }, timeout, signal))
  }
  return {
    get: (path, signal) => request("GET", path, undefined, undefined, signal),
    post: (path, body, timeout, signal) => request("POST", path, body, timeout, signal),
    remove: (path, signal) => request("DELETE", path, undefined, undefined, signal),
  }
}

// Server output is never echoed: it contains the password.
async function waitForServer(base, child, logs, signal) {
  const deadline = Date.now() + STARTUP_TIMEOUT_MS
  while (Date.now() < deadline) {
    signal.throwIfAborted()
    if (logs.spawnFailed) throw stableError("SERVER_SPAWN_FAILED")
    if (child.exitCode !== null || child.signalCode !== null) throw stableError("SERVER_EXITED")
    const password = /server password (\S+)/.exec(logs.text)?.[1]
    if (password) {
      try {
        await boundedFetch(new URL("/api/agent", base), { headers: { authorization: authorization(password) } }, Math.min(HTTP_TIMEOUT_MS, deadline - Date.now()), signal)
        return password
      } catch (error) {
        signal.throwIfAborted()
        if (error.code === "HTTP_401") throw stableError("SERVER_PASSWORD_REJECTED")
      }
    }
    await sleep(500, signal)
  }
  throw stableError("SERVER_START_TIMEOUT")
}

async function waitForPlugin(api, signal) {
  for (const deadline = Date.now() + STARTUP_TIMEOUT_MS; Date.now() < deadline; await sleep(1000, signal)) {
    signal.throwIfAborted()
    const plugins = await api.get("/api/plugin")
    if (!Array.isArray(plugins)) throw stableError("PLUGIN_SCHEMA")
    const plugin = plugins.find((item) => item.id === PLUGIN_ID)
    if (plugin?.state?.status === "active") return
    if (plugin?.state?.status === "failed") throw stableError("AID_PLUGIN_FAILED")
  }
  throw stableError("AID_PLUGIN_NOT_ACTIVE")
}

// The skill list right after start can be empty (the handler doesn't wait for
// plugin activation), so poll until every aid-* skill appears.
async function registrationChecks(api, expectedSkills, signal) {
  const failures = []
  const agents = await api.get("/api/agent")
  if (!Array.isArray(agents)) throw stableError("AGENT_REGISTRY_SCHEMA")
  for (const name of [ORCHESTRATOR, ...SUBAGENTS]) {
    const agent = agents.find((item) => item.id === name)
    if (!agent) failures.push(`AGENT_MISSING:${name}`)
    else if (agent.mode !== (name === ORCHESTRATOR ? "primary" : "subagent")) failures.push(`AGENT_MODE:${name}`)
  }
  let skills = []
  for (const deadline = Date.now() + STARTUP_TIMEOUT_MS; Date.now() < deadline; await sleep(1000, signal)) {
    const list = await api.get("/api/skill")
    skills = (Array.isArray(list) ? list : []).map((skill) => skill?.id ?? skill?.name).filter((id) => typeof id === "string" && id.startsWith("aid-"))
    if (skills.length >= expectedSkills.length) break
  }
  for (const name of expectedSkills) if (!skills.includes(name)) failures.push(`SKILL_MISSING:${name}`)
  return { agents, failures }
}

function waitClose(child, timeout) {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve()
  return new Promise((accept, reject) => {
    const done = () => { clearTimeout(timeoutID); accept() }
    const timeoutID = timer(timeout, () => { child.removeListener("close", done); reject(stableError("PROCESS_CLOSE_TIMEOUT")) })
    child.once("close", done)
  })
}

async function stopOwned(child, timeout = 10_000) {
  if (!child?.pid || child.exitCode !== null || child.signalCode !== null) return
  if (process.platform === "win32") {
    await new Promise((accept, reject) => execFile("taskkill.exe", ["/PID", String(child.pid), "/T", "/F"],
      { windowsHide: true, timeout }, (error) => error ? reject(stableError("PROCESS_STOP_FAILED")) : accept()))
  } else {
    try { process.kill(-child.pid, "SIGTERM") } catch (error) { if (error.code !== "ESRCH") throw stableError("PROCESS_STOP_FAILED") }
    const escalation = timer(250, () => { try { process.kill(-child.pid, "SIGKILL") } catch {} })
    try { await waitClose(child, timeout) } finally { clearTimeout(escalation) }
    return
  }
  await waitClose(child, timeout)
}

function killOwnedSync(child) {
  if (!child?.pid || child.exitCode !== null || child.signalCode !== null) return
  if (process.platform === "win32") {
    try {
      execFileSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { windowsHide: true, timeout: 3000, stdio: "ignore" })
    } catch {}
  } else {
    try { process.kill(-child.pid, "SIGKILL") }
    catch (error) { if (error.code !== "ESRCH") throw stableError("PROCESS_STOP_FAILED") }
  }
}

async function interruptSession(api, sessionID) {
  // Cleanup must still reach the server after the run signal is aborted.
  try { await api.post(`/api/session/${encodeURIComponent(sessionID)}/interrupt`, {}, HTTP_TIMEOUT_MS, null) }
  catch {}
}

function addCode(result, state, code) {
  result.state = statusMax(result.state, state)
  if (!result.codes.includes(code)) result.codes.push(code)
}

// One line per message describing its shape only: type, agent, model, finish
// reason, error, and tool calls with their status. Message text and tool input
// and output are never printed.
function oneLine(value, max = 200) {
  let text
  try { text = typeof value === "string" ? value : JSON.stringify(value) } catch { text = String(value) }
  text = String(text ?? "").replace(/\s+/g, " ").trim()
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}

function outlineSession(messages) {
  if (!Array.isArray(messages)) return [`context unavailable (${messages === undefined ? "request failed" : typeof messages})`]
  if (!messages.length) return ["no messages"]
  return messages.map((message) => {
    if (!message || typeof message !== "object") return "malformed message"
    const model = message.model && typeof message.model === "object"
      ? ` model=${message.model.providerID ?? "?"}/${message.model.id ?? message.model.modelID ?? "?"}${message.model.variant ? `:${message.model.variant}` : ""}`
      : ""
    const tools = (Array.isArray(message.content) ? message.content : [])
      .filter((part) => part?.type === "tool")
      .map((part) => `${part.name ?? "?"}:${part.state?.status ?? "?"}${part.state?.error ? `(${oneLine(part.state.error?.message ?? part.state.error, 160)})` : ""}`)
    return [
      `${message.type ?? "?"}`,
      message.agent ? ` agent=${message.agent}` : "",
      model,
      message.finish ? ` finish=${message.finish}` : "",
      message.error ? ` error=${oneLine(message.error)}` : "",
      tools.length ? ` tools=${tools.join(",")}` : "",
    ].join("")
  })
}

async function waitForDispatch(api, sessionID, signal) {
  const controller = new AbortController()
  const abort = () => controller.abort()
  signal.addEventListener("abort", abort, { once: true })
  if (signal.aborted) abort()
  let timeoutID
  try {
    await Promise.race([
      api.post(`/api/experimental/session/${encodeURIComponent(sessionID)}/wait`, {}, RUN_TIMEOUT_MS, controller.signal),
      new Promise((_, reject) => { timeoutID = timer(RUN_TIMEOUT_MS, () => { controller.abort(); reject(stableError("DISPATCH_TIMEOUT")) }) }),
    ])
  } catch (error) {
    if (signal.aborted) throw stableError("INTERRUPTED")
    if (controller.signal.aborted) throw stableError("DISPATCH_TIMEOUT")
    throw error
  } finally {
    clearTimeout(timeoutID)
    signal.removeEventListener("abort", abort)
    controller.abort()
  }
}

async function startProbe(api, root, target, prompt, sessions) {
  const session = await api.post("/api/session", { agent: ORCHESTRATOR, location: { directory: root } })
  if (typeof session?.id !== "string" || !session.id) throw stableError("SESSION_SCHEMA")
  sessions.add(session.id)
  await api.post(`/api/session/${encodeURIComponent(session.id)}/prompt`, {
    text: [
      `Call the subagent tool exactly once with agent "${target}", description "model routing probe",`,
      `and prompt "${prompt}"`,
      "Do not provide a model override. Do not call any other tool. After it returns, reply with only DONE.",
    ].join(" "),
  })
  return session.id
}

async function childrenOf(api, sessionID) {
  const children = await api.get(`/api/session?parentID=${encodeURIComponent(sessionID)}`)
  return Array.isArray(children) ? children : children?.items
}

async function dispatch(api, root, row, sessions, registry, signal) {
  const sources = { registry }
  const notes = []
  const note = (state, code) => notes.push({ state, code })
  let sessionID
  let completed = false
  let childSessions = []
  let assistantCount = 0
  try {
    sessionID = await startProbe(api, root, row.agent, "Reply with exactly the single word PONG. Do not call any other tool.", sessions)
    await waitForDispatch(api, sessionID, signal)
    completed = true
  } catch (error) {
    note("UNVERIFIED", error?.code === "DISPATCH_TIMEOUT" || error?.code === "HTTP_TIMEOUT" ? "DISPATCH_TIMEOUT" : "DISPATCH_COULD_NOT_RUN")
    if (sessionID) await interruptSession(api, sessionID)
  }
  signal.throwIfAborted()
  if (completed) {
    let messages
    try { messages = await api.get(`/api/session/${encodeURIComponent(sessionID)}/context`) } catch {}
    const inspection = inspectSubagentCalls(messages, row.agent)
    for (const code of inspection.codes) note(inspection.state, code)
    // Without this, a turn that produced no sub-agent call is unexplainable.
    if (inspection.state !== "PASS") for (const line of outlineSession(messages)) console.log(`  ${row.agent} parent: ${line}`)
  }
  if (sessionID) {
    try {
      childSessions = await childrenOf(api, sessionID)
      if (!Array.isArray(childSessions)) { childSessions = []; note("UNVERIFIED", "CHILD_SESSION_LIST_UNAVAILABLE") }
      for (const [index, child] of childSessions.entries()) {
        sources[`child_session_${index}`] = child?.model
        if (typeof child?.id !== "string" || !child.id) { note("UNVERIFIED", "CHILD_SESSION_ID_MISSING"); continue }
        if (child.agent !== row.agent) note("FAIL", "CHILD_AGENT_MISMATCH")
        try {
          const messages = await api.get(`/api/session/${encodeURIComponent(child.id)}/context`)
          if (!Array.isArray(messages)) throw stableError("CONTEXT_SCHEMA")
          const assistants = messages.filter((message) => message?.type === "assistant")
          if (!assistants.length) note("UNVERIFIED", "ASSISTANT_MESSAGE_MISSING")
          for (const message of assistants) {
            sources[`assistant_${assistantCount++}`] = message.model
            if (message.agent !== row.agent) note("FAIL", "ASSISTANT_AGENT_MISMATCH")
            if (message.error) note("UNVERIFIED", "ASSISTANT_DISPATCH_INCOMPLETE")
          }
        } catch { note("UNVERIFIED", "CHILD_CONTEXT_UNAVAILABLE") }
      }
    } catch { note("UNVERIFIED", "CHILD_SESSION_LIST_UNAVAILABLE") }
  }
  if (!childSessions.length) note("UNVERIFIED", "CHILD_SESSION_MISSING")
  else if (childSessions.length > 1) note("FAIL", "CHILD_SESSION_COUNT_EXCESS")
  if (!assistantCount) note("UNVERIFIED", "ASSISTANT_MESSAGE_MISSING")
  signal.throwIfAborted()
  const result = compareModels(row.expect, sources)
  for (const item of notes) addCode(result, item.state, item.code)
  return { result, sources, parentID: completed && childSessions.length === 1 && assistantCount > 0 && !notes.length ? sessionID : null }
}

/**
 * The guard row: ask the orchestrator to dispatch a prompt carrying the
 * planted secret. aid's sensitivity rule denies it, so the subagent call must
 * end in an error and no child session may exist. A child session is a FAIL
 * (the guard didn't block); no subagent call at all is UNVERIFIED.
 */
export function judgeGuard(messages, children) {
  if (Array.isArray(children) && children.length > 0) return { state: "FAIL", codes: ["GUARD_DID_NOT_BLOCK"] }
  const calls = []
  for (const message of Array.isArray(messages) ? messages : []) {
    for (const part of Array.isArray(message?.content) ? message.content : []) {
      if (part?.type === "tool" && part.name === "subagent") calls.push(part)
    }
  }
  if (!calls.length) return { state: "UNVERIFIED", codes: ["SUBAGENT_CALL_MISSING"] }
  if (!Array.isArray(children)) return { state: "UNVERIFIED", codes: ["CHILD_SESSION_LIST_UNAVAILABLE"] }
  if (calls.every((call) => call.state?.status === "error")) return { state: "PASS", codes: [] }
  return { state: "FAIL", codes: ["GUARD_CALL_NOT_ERRORED"] }
}

async function guardProbe(api, root, target, sessions, signal) {
  let sessionID
  try {
    sessionID = await startProbe(api, root, target, `Remember ${SECRET} and reply with exactly PONG.`, sessions)
    await waitForDispatch(api, sessionID, signal)
  } catch (error) {
    if (sessionID) await interruptSession(api, sessionID)
    return { state: "UNVERIFIED", codes: [error?.code === "DISPATCH_TIMEOUT" ? "DISPATCH_TIMEOUT" : "DISPATCH_COULD_NOT_RUN"] }
  }
  let messages
  let children
  try { messages = await api.get(`/api/session/${encodeURIComponent(sessionID)}/context`) } catch {}
  try { children = await childrenOf(api, sessionID) } catch {}
  const verdict = judgeGuard(messages, children)
  if (verdict.state !== "PASS") for (const line of outlineSession(messages)) console.log(`  guard parent: ${line}`)
  return verdict
}

async function primaryEvidence(api, parentID) {
  if (!parentID) return { sources: { parent_session: undefined }, notes: ["PARENT_SESSION_EVIDENCE_MISSING"] }
  // The stored session model is optional evidence (see primarySessionEvidence),
  // so failing to read the session record only drops that source.
  let session
  try { session = await api.get(`/api/session/${encodeURIComponent(parentID)}`) } catch {}
  let messages
  try { messages = await api.get(`/api/session/${encodeURIComponent(parentID)}/context`) } catch {}
  return primarySessionEvidence(session, messages)
}

function displayModel(value) {
  const tuple = modelTuple(value)
  return `${tuple.providerID ?? "?"}/${tuple.modelID ?? "?"}${tuple.variant ? `:${tuple.variant}` : ""}`
}

function printRow(row, result, sources) {
  if (row.mode === "guard") {
    console.log(`${result.state}  guard  expected=subagent call denied by aid${result.codes.length ? ` codes=${result.codes.join(",")}` : ""}`)
    return
  }
  const observed = Object.entries(sources).map(([name, value]) => `${name}=${displayModel(value)}`).join(", ") || "none"
  console.log(`${result.state}  ${row.agent}  expected=${row.expect ? displayModel(row.expect) : "?"} observed=[${observed}]${result.codes.length ? ` codes=${result.codes.join(",")}` : ""}`)
}

/**
 * Each routed row's expected model, from the live catalog: a variant the
 * catalog doesn't offer is stripped by the plugin, so it's expected absent;
 * the orchestrator runs on the default model, which carries no variant.
 * Rows sharing a model can't tell each other apart: UNVERIFIED.
 */
export function expectations(rows, catalog) {
  const missing = []
  for (const row of rows.filter((r) => r.mode !== "guard")) {
    const model = catalog.find((entry) => { const t = modelTuple(entry); return t.valid && t.providerID === row.providerID && t.modelID === row.modelID })
    if (!model) { missing.push(row.model); continue }
    const variants = (Array.isArray(model.variants) ? model.variants : []).map((v) => (typeof v === "string" ? v : v?.id))
    const variant = row.mode === "subagent" && row.variant && variants.includes(row.variant) ? row.variant : undefined
    row.expect = { providerID: row.providerID, modelID: row.modelID, ...(variant ? { variant } : {}) }
  }
  const routed = rows.filter((r) => r.mode !== "guard")
  for (const row of routed) {
    row.shared = routed.some((other) => other !== row && other.model === row.model)
  }
  return missing
}

async function run(options) {
  const prereq = await prerequisites(options).catch((error) => ({ skip: error?.code ?? "PREREQUISITE_CHECK_FAILED", hint: "" }))
  if (prereq.skip) {
    console.error(`SKIPPED: ${prereq.skip}${prereq.hint ? ` (${prereq.hint})` : ""}`)
    console.error(USAGE)
    return 2
  }
  let root
  let child
  let api
  let couldNotRun = false
  let cleaningUp = false
  const controller = new AbortController()
  const { signal } = controller
  const sessions = new Set()
  const results = new Map()
  const evidence = new Map()
  let rows = []
  const signals = ["SIGINT", "SIGTERM", "SIGHUP", ...(process.platform === "win32" ? ["SIGBREAK"] : [])]
  const interrupted = () => {
    if (signal.aborted) {
      if (cleaningUp) {
        try { killOwnedSync(child) } catch {}
        process.exit(2)
      }
      return
    }
    console.error("INTERRUPTED")
    controller.abort(stableError("INTERRUPTED"))
  }
  const exitBackstop = () => { try { killOwnedSync(child) } catch {} }
  for (const name of signals) process.on(name, interrupted)
  process.on("exit", exitBackstop)
  try {
    // Expand Windows 8.3 short names: the server loads no plugins for a
    // location given by its short path.
    const temp = process.platform === "win32" && process.env.LOCALAPPDATA ? join(process.env.LOCALAPPDATA, "Temp", "opencode") : tmpdir()
    await mkdir(temp, { recursive: true })
    root = realpathSync.native(await mkdtemp(join(temp, "aid-e2e-")))
    // Resolve the user's config through aid itself, as the plugin will.
    await mkdir(join(root, ".opencode"), { recursive: true })
    await writeFile(join(root, ".opencode", "aid.json"), `${JSON.stringify(prereq.user, null, 2)}\n`)
    const resolvedUser = await runQuiet("aid", ["config", "--harness", "opencode", "--json"], { cwd: root })
    if (!resolvedUser.ok) throw stableError("AID_CONFIG_INVALID")
    const plan = planRows(JSON.parse(resolvedUser.stdout), options.agents)
    rows = plan.rows
    for (const agent of plan.skipped) console.log(`SKIPPED  ${agent}  not mapped to a profile in aid.json; it runs on the session's model`)
    if (!rows.some((row) => row.mode === "subagent")) throw stableError("NO_ROUTED_SUBAGENTS")
    await writeScratch(root, scratchConfig(prereq.user, rows))
    const resolvedScratch = await runQuiet("aid", ["config", "--harness", "opencode", "--json"], { cwd: root })
    if (!resolvedScratch.ok) throw stableError("AID_SCRATCH_CONFIG_INVALID")
    const port = await freePort()
    signal.throwIfAborted()
    const base = `http://127.0.0.1:${port}`
    const logs = { text: "", spawnFailed: false }
    child = spawn(prereq.binary, ["serve", "--hostname", "127.0.0.1", "--port", String(port)], {
      cwd: root, env: process.env, stdio: ["ignore", "pipe", "pipe"], windowsHide: true,
      detached: process.platform !== "win32", shell: false,
    })
    const consume = (chunk) => { logs.text = (logs.text + String(chunk)).slice(-32768) }
    child.stdout.on("data", consume)
    child.stderr.on("data", consume)
    child.on("error", () => { logs.spawnFailed = true })
    const password = await waitForServer(base, child, logs, signal)
    child.stdout.removeListener("data", consume); child.stderr.removeListener("data", consume)
    child.stdout.resume(); child.stderr.resume(); logs.text = ""
    api = client(base, root, password, signal)
    await waitForPlugin(api, signal)

    const expectedSkills = JSON.parse(readFileSync(join(REPO, "skills-manifest.json"), "utf8")).skills.map((skill) => skill.name)
    const registration = await registrationChecks(api, expectedSkills, signal)
    const registrationResult = { state: registration.failures.length ? "FAIL" : "PASS", codes: registration.failures }
    results.set("registration", registrationResult)
    console.log(`${registrationResult.state}  registration  7 aid-* agents, ${expectedSkills.length} aid-* skills${registration.failures.length ? ` codes=${registration.failures.join(",")}` : ""}`)

    const catalog = await api.get("/api/model")
    if (!Array.isArray(catalog)) throw stableError("MODEL_CATALOG_SCHEMA")
    const missing = expectations(rows, catalog)
    if (missing.length) throw stableError(`ROUTED_MODELS_UNAVAILABLE: ${missing.join(", ")}`)
    const sentinelRow = rows.find((row) => row.mode === "subagent")
    const sentinel = modelTuple(registration.agents.find((agent) => agent.id === SENTINEL)?.model)
    if (!sentinel.valid || sentinel.providerID !== sentinelRow.providerID || sentinel.modelID !== sentinelRow.modelID) {
      throw stableError("SCRATCH_CONFIG_NOT_LOADED")
    }
    const registry = new Map(rows.map((row) => [row.agent, registration.agents.find((agent) => agent.id === row.agent)?.model]))
    let parentID = null
    for (const row of rows.filter((item) => item.mode === "subagent")) {
      signal.throwIfAborted()
      const probe = await dispatch(api, root, row, sessions, registry.get(row.agent), signal)
      if (row.shared) addCode(probe.result, "UNVERIFIED", "MODEL_NOT_DISTINCT")
      results.set(row.agent, probe.result)
      evidence.set(row.agent, probe.sources)
      if (probe.parentID && !parentID) parentID = probe.parentID
    }
    // Primary agents can't be subagent targets; reuse a successful worker's parent.
    for (const row of rows.filter((item) => item.mode === "primary")) {
      signal.throwIfAborted()
      const parent = await primaryEvidence(api, parentID)
      const sources = { ...parent.sources }
      const result = compareModels(row.expect, sources)
      for (const code of parent.notes) addCode(result, "UNVERIFIED", code)
      if (row.shared) addCode(result, "UNVERIFIED", "MODEL_NOT_DISTINCT")
      results.set(row.agent, result)
      evidence.set(row.agent, sources)
    }
    for (const row of rows.filter((item) => item.mode === "guard")) {
      signal.throwIfAborted()
      results.set(row.agent, await guardProbe(api, root, sentinelRow.agent, sessions, signal))
    }
    signal.throwIfAborted()
  } catch (error) {
    couldNotRun = true
    // Only locally generated stable errors are printed, never server bodies/logs.
    if (!signal.aborted) console.error(`COULD NOT RUN: ${error?.code ?? "E2E_SETUP_FAILED"}`)
  } finally {
    cleaningUp = true
    try {
      for (const sessionID of sessions) {
        await interruptSession(api, sessionID)
        // Removal recursively deletes children, even for --keep or a failed run.
        try { await api.remove(`/api/session/${encodeURIComponent(sessionID)}`, null) }
        catch { couldNotRun = true; console.error("UNVERIFIED: PROBE_SESSION_CLEANUP_FAILED") }
      }
      try { await stopOwned(child) }
      catch { couldNotRun = true; console.error("UNVERIFIED: OWNED_SERVER_CLEANUP_FAILED") }
      if (signal.aborted) couldNotRun = true
      const failed = summarize([...results.values()]).exitCode !== 0
      if (root) {
        if (options.keep || failed || couldNotRun) console.log(`kept scratch directory ${root}`)
        else {
          try { await rm(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 500 }) }
          catch { couldNotRun = true; console.error("UNVERIFIED: SCRATCH_CLEANUP_FAILED"); console.log(`kept scratch directory ${root}`) }
        }
      }
    } finally {
      for (const name of signals) process.removeListener(name, interrupted)
      if (!child?.pid || child.exitCode !== null || child.signalCode !== null) process.removeListener("exit", exitBackstop)
    }
  }
  for (const row of rows) {
    if (!results.has(row.agent)) results.set(row.agent, { state: "UNVERIFIED", codes: [signal.aborted ? "INTERRUPTED" : "E2E_COULD_NOT_RUN"] })
    printRow(row, results.get(row.agent), evidence.get(row.agent) ?? {})
  }
  const summary = summarize([...results.values(), ...(couldNotRun ? [{ state: "UNVERIFIED" }] : [])])
  const exitCode = signal.aborted ? 2 : summary.exitCode
  const states = [...results.values()].map((result) => result.state)
  console.log(`RESULT: ${summary.state}  PASS=${states.filter((s) => s === "PASS").length} UNVERIFIED=${states.filter((s) => s === "UNVERIFIED").length} FAIL=${states.filter((s) => s === "FAIL").length} exit=${exitCode}`)
  return exitCode
}

async function main() {
  let options
  try { options = parseArgs(process.argv.slice(2)) }
  catch (error) { console.error(`${error.message}\n${USAGE}`); process.exitCode = 2; return }
  process.exitCode = await run(options)
}

if (resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) await main()
