package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

// AidDir is the directory, at the repository root, that holds every work.
const AidDir = ".aid"

// app is what every command shares: where it runs, the flags, the clock, and
// the output streams. Tests build their own.
type app struct {
	cwd      string
	work     string // --work
	json     bool   // --json
	now      func() time.Time
	out, err io.Writer
}

func defaultApp() (*app, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("can't determine the current directory: %w", err)
	}
	return &app{cwd: cwd, now: clock(), out: os.Stdout, err: os.Stderr}, nil
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:           "aid",
		Short:         "The task graph and state record of a piece of Make work",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.Version = currentVersion().String()
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetOut(a.out)
	root.SetErr(a.err)
	root.PersistentFlags().StringVar(&a.work, "work", "", "the work to act on: .aid/<name>/ (optional when only one exists)")
	root.PersistentFlags().BoolVar(&a.json, "json", false, "machine-readable output")
	for _, add := range commands {
		root.AddCommand(add(a))
	}
	return root
}

// commands is filled by each command's file, so adding a verb touches only
// its own file.
var commands []func(*app) *cobra.Command

// findAid walks up from start to the nearest directory holding `.aid/`. The
// repository root is that directory: aid never asks git.
func findAid(start string) (root string, ok bool) {
	dir := start
	for {
		if fi, err := os.Stat(filepath.Join(dir, AidDir)); err == nil && fi.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// open selects the work a command acts on: --work, or the only one there is.
// It returns the work and the repository root.
func (a *app) open() (store.Work, string, error) {
	root, ok := findAid(a.cwd)
	if !ok {
		return store.Work{}, "", fmt.Errorf("no %s directory here or above; run `aid init <name>` at the repository root", AidDir)
	}
	aidDir := filepath.Join(root, AidDir)
	if a.work != "" {
		w, err := store.Open(aidDir, a.work)
		return w, root, err
	}
	names, err := works(aidDir)
	if err != nil {
		return store.Work{}, "", err
	}
	switch len(names) {
	case 0:
		return store.Work{}, "", fmt.Errorf("no work in %s; run `aid init <name>`", aidDir)
	case 1:
		w, err := store.Open(aidDir, names[0])
		return w, root, err
	}
	return store.Work{}, "", fmt.Errorf("several works in %s (%s); pick one with --work", aidDir, strings.Join(names, ", "))
}

// works lists the initialised works under aidDir: directories holding a task
// graph. A directory with only a breakdown, written before `aid init`, isn't
// one yet.
func works(aidDir string) ([]string, error) {
	entries, err := os.ReadDir(aidDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(aidDir, e.Name(), graph.File)); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// emit writes v as JSON with --json, otherwise the text.
func (a *app) emit(v any, text string) error {
	if !a.json {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		_, err := io.WriteString(a.out, text)
		return err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := a.out.Write(b.Bytes())
	return err
}

// refusal is an error that carries structured detail for --json callers, so
// an agent can act on a refusal without parsing prose.
type refusal struct {
	msg    string
	detail any
}

func (r *refusal) Error() string { return r.msg }

func asRefusal(err error) (*refusal, bool) {
	var r *refusal
	return r, errors.As(err, &r)
}
