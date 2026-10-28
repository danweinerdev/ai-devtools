package guard

import (
	"encoding/json"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The definition-of-done gate. A shell call that runs `aid update-task` and
// would weaken a task's definition of done is put to the user ("ask"), so an
// agent never approves its own weakening. The rule itself lives with the
// tool: the guard parses the call and hands its arguments to env.DoneGate,
// which judges them against the work's current graph. A call the guard can't
// parse with confidence that passes --check or --result is asked too, and
// file-tool edits of the graph or state file are denied, so a direct edit
// can't skip the tool's refusals. It applies to subagents as much as to the
// main loop: workers are the ones a weakening would serve.

// DoneGate judges `aid <args…>` run from cwd: the task it would weaken and
// what it weakens, or "" when it weakens nothing. An error (no .aid, no such
// task, bad flags) means aid would refuse the call itself, so it passes.
type DoneGate func(cwd string, args []string) (task, weakens string, err error)

// editTools are the file tools whose target the gate checks. OpenCode's
// patch names its files inside the patch text.
var editTools = []string{"Write", "Edit", "MultiEdit", "NotebookEdit", "Patch"}

// aidRecords are the files under .aid/<work>/ that only the tool may write.
var aidRecords = []string{"tasks.json", "state.json"}

// doneFlags is a call that could change a definition of done.
var doneFlags = regexp.MustCompile(`--(check|result)\b`)

// patchFiles are the file headers of an OpenCode patch.
var patchFiles = regexp.MustCompile(`(?m)^\*\*\* (?:Add File|Delete File|Update File|Move to): (.+?)\s*$`)

func definitionOfDone(p *Object, env Env) (Decision, error) {
	if p == nil {
		return Decision{}, nil
	}
	tool := str(p.Get("tool_name"))
	in := toolInput(p)
	cwd := cwdOf(p, env)
	if slices.Contains(editTools, tool) {
		for _, target := range editTargets(tool, in) {
			if isAidRecord(joinPath(cwd, expandUser(target, env.Home))) {
				return deny("the task graph and state record (.aid/<work>/tasks.json and state.json) change only " +
					"through the aid tool, so its refusals and the definition-of-done history apply. Use the aid " +
					"command that makes the change (update-task, add-item, resolve-item, …)."), nil
			}
		}
		return Decision{}, nil
	}
	if tool != "Bash" {
		return Decision{}, nil
	}
	command := str(in.Get("command"))
	if !strings.Contains(command, "update-task") {
		return Decision{}, nil
	}
	argv, ok := simpleCommand(command)
	if !ok || env.DoneGate == nil {
		if doneFlags.MatchString(command) {
			return ask("", "this command runs `aid update-task` with --check or --result in a form the guard can't "+
				"read (variables, substitutions, or several commands chained together), so it can't tell whether "+
				"it weakens a task's definition of done. Approve it only if it doesn't, or run `aid update-task` "+
				"on its own so the guard can check it.")
		}
		return Decision{}, nil
	}
	if len(argv) == 0 || !isAidBinary(argv[0]) {
		return Decision{}, nil
	}
	task, weakens, err := env.DoneGate(cwd, argv[1:])
	if err != nil || weakens == "" {
		return Decision{}, nil
	}
	return ask(task, "this `aid update-task` call "+weakens+" of task "+task+", which weakens its definition of "+
		"done. An agent never approves its own weakening: approve it only if the reason given holds.")
}

// ask puts the call to the user: Claude Code's "ask" permission decision,
// and Ask/Task for OpenCode's permission request.
func ask(task, reason string) (Decision, error) {
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "ask",
		"permissionDecisionReason": "aid definition-of-done gate: " + reason,
	}})
	if err != nil {
		return Decision{}, err
	}
	return Decision{Stdout: string(out) + "\n", Ask: reason, Task: task}, nil
}

// editTargets are the paths a file tool writes.
func editTargets(tool string, in *Object) []string {
	if tool == "Patch" {
		var out []string
		for _, m := range patchFiles.FindAllStringSubmatch(str(in.Get("patchText")), -1) {
			out = append(out, m[1])
		}
		return out
	}
	var out []string
	for _, field := range pathFields {
		if s := str(in.Get(field)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isAidRecord: path is a graph or state file directly inside a work
// directory, .aid/<work>/.
func isAidRecord(file string) bool {
	clean := filepath.ToSlash(filepath.Clean(file))
	parts := strings.Split(clean, "/")
	n := len(parts)
	return n >= 3 && parts[n-3] == ".aid" && slices.Contains(aidRecords, parts[n-1])
}

// isAidBinary: the word runs aid, by name or by path.
func isAidBinary(word string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(word, `\`, "/")))
	return base == "aid" || base == "aid.exe"
}

// simpleCommand splits a shell command into words when it is one plain
// command: literal words, single quotes, and double quotes without
// expansions, optionally after literal VAR=value assignments. Anything else
// (separators, pipes, redirects, expansions, globs, escapes, comments,
// newlines) is refused, because its effect can't be read off the text.
func simpleCommand(command string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	inWord := false
	const (
		plain = iota
		single
		double
	)
	mode := plain
	for _, r := range command {
		switch mode {
		case single:
			if r == '\'' {
				mode = plain
			} else {
				cur.WriteRune(r)
			}
			continue
		case double:
			switch r {
			case '"':
				mode = plain
			case '$', '`', '\\', '!':
				return nil, false
			default:
				cur.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '\'':
			mode, inWord = single, true
		case r == '"':
			mode, inWord = double, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case strings.ContainsRune(";&|<>()`$*?[]{}~!#\\\n\r", r):
			return nil, false
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if mode != plain {
		return nil, false
	}
	if inWord {
		words = append(words, cur.String())
	}
	for len(words) > 0 && assignment.MatchString(words[0]) {
		words = words[1:]
	}
	return words, true
}

// assignment is a leading VAR=value word.
var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
