// Command plugin-compiler generates every harness-specific artifact from the
// canonical sources.
//
//	skills/<name>/SKILL.md   canonical, consumed as-is by all three harnesses
//	agents/<name>.md         canonical, Claude-native frontmatter plus neutral keys
//
// Outputs (committed, drift-gated by --check):
//
//	skills-manifest.json, agents-manifest.json      harness-neutral registries
//	dist/opencode/agents/<name>.md                  OpenCode V2 agent files
//	dist/codex/agents/<name>.toml                   Codex agent role files
//
// The OpenCode orchestrator is the one agent built from a skill: its body is
// the aid-orchestrate skill, so the orchestration rules live in one place.
//
// Only agent frontmatter is transformed. Prose is never rewritten: a skill or
// agent body must read correctly in every harness as written.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "fail if generated files differ from a fresh build, writing nothing")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	os.Exit(run(*root, *check, os.Stdout, os.Stderr))
}

func run(root string, check bool, stdout, stderr io.Writer) int {
	p, errs := load(root)
	// Report every validation error before rendering, which assumes valid input.
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(stderr, "error: "+e)
		}
		return 1
	}
	stale, err := sync(root, render(p), check)
	if err != nil {
		fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}
	if len(stale) > 0 {
		fmt.Fprintf(stderr, "generated files are stale — run `make build`:\n  %s\n", strings.Join(stale, "\n  "))
		return 1
	}
	verb := "built"
	if check {
		verb = "checked"
	}
	fmt.Fprintf(stdout, "%s: %d skills, %d agents (%s)\n", verb, len(p.skills), len(p.agents), filepath.Clean(root))
	return 0
}
