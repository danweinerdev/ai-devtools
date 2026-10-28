package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The inline-exploration budget. After guards.inline_budget (default 20)
// search-type tool calls in one user turn, further ones are denied with a
// pointer to aid-explorer. A foreground Agent call resets the budget; a
// background one doesn't. Subagent tool calls are never counted.
//
// Not counted:
//   - verification and inspection commands (tests, linters, git log/show,
//     process inspection, counting pipes; see isVerification),
//   - operations whose every path is under docs/PRDs/ or .aid/, or is
//     .claude/aid.json itself,
//   - aid CLI invocations.
//
// The patterns are RE2, which has no lookaround; the three rules that would
// need it are code: writeRedirect, grepCounting and paged.

var searchyTools = []string{"Read", "Grep", "Glob"}

// commandStart is a command position: the start, or after ; & | ` $( or a
// newline, with optional VAR=x assignments, so prefixes like `cd x && grep`
// or `export F=1; rg` can't dodge the counter. Words in ordinary arguments
// don't match.
const commandStart = "(?:^|[;&|`\\n]|\\$\\()\\s*(?:\\w+=\\S*\\s+)*"

var (
	// `git grep` is a code search, not history.
	searchyBash = regexp.MustCompile(commandStart +
		`(?:git\s+(?:-\w+\s+|--\S+\s+)*)?(rg|grep|find|fd|ls|tree|cat|head|tail|awk|jq|yq)\b`)

	// Path-shaped argument tokens: anything with a separator, plus bare
	// capitalised words that could name an artifact directory (Plans, Specs).
	// A token wrongly judged "not a path" only makes the guard stricter.
	// Word characters are Unicode: an ASCII-only class would miss
	// `src/Ünï.go` and let the rest of the command pass as exempt.
	pathToken = regexp.MustCompile(`^[~./]?[\p{L}\p{N}_.@+-]+(?:/[\p{L}\p{N}_.*?@+-]*)+/?$|^[A-Z][\p{L}\p{N}_-]*/?$`)

	// Verification and inspection. The budget polices exploration: answering
	// means ingesting far more text than the answer contains. Verification is
	// the opposite shape, so counting it measures the wrong thing.
	//
	// Machine state: a process check, a disk sample, a memory reading.
	inspectCmd = regexp.MustCompile(commandStart +
		`(ps|top|htop|du|df|free|uptime|uname|whoami|id|date|lsof|ss|netstat|` +
		`nproc|stat|file|wc|which|command|type|env|printenv|sha\d*sum|md5sum|` +
		`cksum|b2sum)\b`)
	// Reading /proc or /sys is a machine-state probe whatever tool does it.
	procRead = regexp.MustCompile(`(?:^|[\s'"=])/(?:proc|sys)/`)
	// Repository history verifies a claim about what changed. `git grep` is
	// deliberately absent: it's a code search wearing a git prefix.
	gitVerify = regexp.MustCompile(commandStart + `git\s+(?:-\w+\s+|--\S+\s+)*` +
		`(show|log|diff|rev-parse|status|blame|describe|tag|branch|remote|` +
		`cat-file|shortlog|reflog|check-ignore|ls-remote)\b`)
	// Test runners, linters, type checkers, validators. Dedicated tools match
	// on their own name; interpreters and multi-purpose build tools must name
	// a test or lint subcommand, or any script could launder exploration
	// through them. jq and yq are absent: reading a document through them is
	// exploration.
	verifyCmd = regexp.MustCompile(commandStart + `(?:` +
		`(?:pytest|py\.test|tox|nox|nosetests|jest|vitest|mocha|ava|` +
		`jasmine|karma|phpunit|rspec|minitest|ctest|gtest|` +
		`ruff|flake8|pylint|black|isort|mypy|pyright|pyre|bandit|vulture|` +
		`eslint|prettier|tsc|biome|stylelint|` +
		`rubocop|golangci-lint|gofmt|govet|staticcheck|clippy|rustfmt|` +
		`clang-tidy|clang-format|cppcheck|shellcheck|shfmt|hadolint|` +
		`tflint|kubeconform|kubeval|ansible-lint|yamllint|xmllint)\b` +
		`|python3?\s+-m\s+(?:pytest|unittest|tox|nox|mypy|ruff|flake8|pylint|` +
		`black|isort|pyright|bandit|compileall)\b` +
		`|(?:npm|pnpm|yarn|bun)\s+(?:run\s+)?(?:test|lint|typecheck|check)\b` +
		`|(?:go|cargo|dotnet|swift|mvn|gradle)\s+(?:test|vet|fmt|check|clippy)\b` +
		`|(?:make|ninja|bazel|just|task)\s+(?:test|check|lint|verify)\b` +
		`|terraform\s+(?:validate|fmt)\b` +
		`|helm\s+(?:lint|template)\b` +
		`)`)
	// `... | wc -l` is counting, not reading: one number however much was
	// scanned.
	countingPipe = regexp.MustCompile(`\|\s*(?:\w+=\S*\s+)*wc\b(?:\s+-[lcwm]+)?\s*$`)
	// `grep -c` counts; `-l`/`-L` list file names. Matching anywhere in a
	// clause, the flag can simply be part of the match.
	grepCounting = regexp.MustCompile("(?:^|[;&|`\\n]|\\$\\()\\s*(?:rg|grep|egrep|fgrep)\\b" +
		`(?:\s+-\S+)*\s+-\w*[clL]`)
	// A pipe into a pager or a filter: see paged.
	pipeInto = regexp.MustCompile(`\|\s*(?:\w+=\S*\s+)*(head|tail|less|more|grep|egrep|fgrep|rg)\b`)
	// countingFlags, anchored at the end of a pipe-into match, spares a
	// counting grep (`ps -ef | grep -c python`).
	countingFlags = regexp.MustCompile(`^(?:\s+-\S+)*\s+-\w*[clL]\b`)
	// A compound command is verification only if every clause is.
	clauseSplit = regexp.MustCompile(`(?:\|\||&&|[;&\n])`)
	// `aid ...` at a command position.
	aidCLI = regexp.MustCompile(`^\s*(?:\w+=\S*\s+)*aid(?:\s|$)`)
	// Argument separators when pulling path tokens out of a command.
	argSplit = regexp.MustCompile(`[\s;|&]+`)
)

var pathFields = []string{"file_path", "path", "notebook_path", "filePath"}

// exemptRoots, under the session's directory, are never counted.
var exemptRoots = [][]string{{"docs", "PRDs"}, {".aid"}, {".claude", "aid.json"}, {".opencode", "aid.json"}}

// writeRedirect: stdout redirected to a file means the command writes, it
// doesn't explore. `2>` and `2>&1` are stderr plumbing common in real
// searches and still count.
func writeRedirect(cmd string) bool {
	if strings.Contains(cmd, "&>") {
		return true
	}
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '>' {
			continue
		}
		if i == 0 || !(cmd[i-1] >= '0' && cmd[i-1] <= '9' || cmd[i-1] == '&') {
			return true
		}
	}
	return false
}

// paged: a listing long enough to need paging is too long to be the answer,
// so a pipe into head, tail, less, more, or a filtering grep sinks the whole
// command, except a counting grep. sort, awk and cut reshape a fixed-size
// table and are absent.
func paged(cmd string) bool {
	for _, m := range pipeInto.FindAllStringSubmatchIndex(cmd, -1) {
		switch cmd[m[2]:m[3]] {
		case "head", "tail", "less", "more":
			return true
		}
		if !countingFlags.MatchString(cmd[m[1]:]) {
			return true
		}
	}
	return false
}

func counterPath(p *Object, tempDir string) string {
	id := func(key string) string {
		if !p.Has(key) {
			return "unknown"
		}
		return display(p.Get(key))
	}
	return filepath.Join(tempDir, fmt.Sprintf("aid-inline-%s-%s", id("session_id"), id("prompt_id")))
}

func inline(p *Object, env Env) (Decision, error) {
	if p == nil {
		return Decision{}, nil
	}
	if truthy(p.Get("agent_id")) || truthy(p.Get("agent_type")) {
		return Decision{}, nil // a subagent doing its job; never throttle workers
	}
	tool := str(p.Get("tool_name"))
	in := toolInput(p)

	if slices.Contains(agentTools, tool) {
		// A foreground dispatch can't be raced, so it resets the budget; a
		// background one returns control at once, so the counter keeps climbing.
		background := true
		if in.Has("run_in_background") {
			background = truthy(in.Get("run_in_background"))
		}
		if !background {
			_ = os.Remove(counterPath(p, env.TempDir))
		}
		return Decision{}, nil
	}
	if !slices.Contains(searchyTools, tool) {
		if tool != "Bash" {
			return Decision{}, nil
		}
		command := str(in.Get("command"))
		if !searchyBash.MatchString(command) {
			return Decision{}, nil
		}
		if writeRedirect(command) {
			return Decision{}, nil // writing a file isn't exploration
		}
	}

	cwd := cwdOf(p, env)
	if exemptPathOp(tool, in, cwd, env.Home) || isAidCLI(tool, in) {
		return Decision{}, nil // before the config load, so a broken aid.json can still be read
	}
	cfg, err := loadConfig(cwd, env)
	if err != nil {
		return Decision{}, err
	}
	if cfg.ExemptVerification && isVerification(tool, in) {
		return Decision{}, nil
	}

	path := counterPath(p, env.TempDir)
	count := 0
	if raw, err := os.ReadFile(path); err == nil {
		count, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	count++
	if err := os.WriteFile(path, []byte(strconv.Itoa(count)), 0o644); err != nil {
		return Decision{}, nil
	}
	if count <= cfg.InlineBudget {
		return Decision{}, nil
	}
	return deny(fmt.Sprintf("inline search op %d this turn exceeds the budget of %d. You are exploring "+
		"inline at main-loop cost. Delegate the remaining discovery to aid-explorer in one %s call; "+
		"the budget resets when you delegate. Reads under docs/PRDs/ and .aid/, aid CLI calls, and "+
		"verification commands (tests, linters, git log/show, process checks) are not counted. To raise "+
		`the budget set {"guards": {"inline_budget": %d}} in %s; AID_GUARDS=off disables `+
		"the guards for the session.", count, cfg.InlineBudget, spawnCall(env), max(cfg.InlineBudget*2, 20),
		cfg.Path)), nil
}

// bashPathTokens are the argument tokens of a shell command that name paths.
func bashPathTokens(command, cwd, home string) []string {
	var tokens []string
	for _, raw := range argSplit.Split(command, -1) {
		token := strings.Trim(raw, "'\"`()")
		if token == "" || strings.HasPrefix(token, "-") {
			continue
		}
		if strings.Contains(token, "=") && !strings.HasPrefix(token, "/") {
			continue
		}
		if pathToken.MatchString(token) {
			tokens = append(tokens, token)
			continue
		}
		// A bare word naming something on disk is a path argument (`grep foo
		// src`), not a search pattern. Mistaking a pattern for a path only
		// makes the guard stricter, so resolve the ambiguity that way.
		if _, err := os.Stat(joinPath(cwd, expandUser(token, home))); err == nil {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

// globLiteralPrefix is the leading path components of a glob with no
// wildcards: `.plans/**/*.md` is `.plans`, `**/*.py` has none. A pattern with
// no literal head stays unscoped and is counted like any broad search.
func globLiteralPrefix(pattern string) string {
	if pattern == "" {
		return ""
	}
	var parts []string
	for _, part := range strings.Split(pattern, "/") {
		if part == "" {
			if len(parts) == 0 {
				parts = append(parts, "") // a leading empty part: absolute
				continue
			}
			break
		}
		if strings.ContainsAny(part, "*?[]{}!") {
			break
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return ""
	}
	return strings.Join(parts, "/")
}

func candidatePaths(tool string, in *Object, cwd, home string) []string {
	if tool == "Bash" {
		return bashPathTokens(str(in.Get("command")), cwd, home)
	}
	var paths []string
	for _, field := range pathFields {
		if s := str(in.Get(field)); s != "" {
			paths = append(paths, s)
		}
	}
	// Glob's pattern is a path glob. Grep's pattern is a regex to search for,
	// never a path; only its glob field filters paths.
	field := map[string]string{"Glob": "pattern", "Grep": "glob"}[tool]
	if field != "" {
		if prefix := globLiteralPrefix(str(in.Get(field))); prefix != "" {
			paths = append(paths, prefix)
		}
	}
	return paths
}

// exemptPathOp: every path the operation touches is under docs/PRDs or .aid,
// or is .claude/aid.json. An operation naming no path is unscoped and counts,
// and one stray path makes the whole operation count.
func exemptPathOp(tool string, in *Object, cwd, home string) bool {
	paths := candidatePaths(tool, in, cwd, home)
	if len(paths) == 0 {
		return false
	}
	base, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}
	for _, p := range paths {
		abs, err := filepath.Abs(joinPath(cwd, expandUser(p, home)))
		if err != nil {
			return false
		}
		if !slices.ContainsFunc(exemptRoots, func(parts []string) bool {
			return under(abs, filepath.Join(append([]string{base}, parts...)...))
		}) {
			return false
		}
	}
	return true
}

func under(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// isAidCLI: every clause of a Bash command is an aid invocation.
func isAidCLI(tool string, in *Object) bool {
	if tool != "Bash" {
		return false
	}
	clauses := splitClauses(str(in.Get("command")))
	return len(clauses) > 0 && !slices.ContainsFunc(clauses, func(c string) bool { return !aidCLI.MatchString(c) })
}

// isVerification: a Bash command that verifies or inspects rather than
// explores, its output about the size of its answer. Exempting is the lax
// direction, so every rule fails closed: a compound command qualifies only if
// every clause does, and a pager anywhere sinks it. The structured tools have
// no verification form.
func isVerification(tool string, in *Object) bool {
	if tool != "Bash" {
		return false
	}
	command := str(in.Get("command"))
	if strings.TrimSpace(command) == "" {
		return false
	}
	// A counting pipe is verification whatever feeds it. Checked before the
	// clause split, which would sever the pipe from the search feeding it.
	if countingPipe.MatchString(command) {
		return true
	}
	if paged(command) {
		return false
	}
	clauses := splitClauses(command)
	if len(clauses) == 0 {
		return false
	}
	return !slices.ContainsFunc(clauses, func(c string) bool {
		return !(grepCounting.MatchString(c) || procRead.MatchString(c) || gitVerify.MatchString(c) ||
			verifyCmd.MatchString(c) || inspectCmd.MatchString(c))
	})
}

func splitClauses(command string) []string {
	var out []string
	for _, c := range clauseSplit.Split(command, -1) {
		if strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
	}
	return out
}

// expandUser expands a leading ~ or ~/ to home.
func expandUser(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// joinPath joins p onto cwd, unless p is already absolute.
func joinPath(cwd, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(cwd, p)
}

// fnmatch is shell-style matching where `*` and `?` cross slashes; `[seq]` and
// `[!seq]` are character classes, and everything else is literal.
func fnmatch(name, pattern string) bool {
	pat := []rune(pattern)
	var b strings.Builder
	b.WriteString("(?s)^")
	for i := 0; i < len(pat); i++ {
		switch c := pat[i]; c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i + 1
			if j < len(pat) && pat[j] == '!' {
				j++
			}
			if j < len(pat) && pat[j] == ']' {
				j++
			}
			for j < len(pat) && pat[j] != ']' {
				j++
			}
			if j >= len(pat) {
				b.WriteString(`\[`)
				continue
			}
			class := string(pat[i+1 : j])
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			} else if strings.HasPrefix(class, "^") {
				class = `\` + class
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = j
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(name)
}
