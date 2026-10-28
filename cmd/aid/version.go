package main

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, versionCmd) }

// version is the plugin version, set at build time from package.json
// (`make install-aid` and `make release` pass -X main.version=…).
var version = ""

// buildVersion is what this binary knows of its version: the version set at
// build time, else the module version `go install …@vX.Y.Z` records, plus
// the git commit and whether the tree had uncommitted changes, when Go
// recorded them.
type buildVersion struct {
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Modified bool   `json:"modified,omitempty"`
}

func currentVersion() buildVersion {
	v := buildVersion{Version: version}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		if v.Version == "" {
			v.Version = "unknown"
		}
		return v
	}
	if v.Version == "" {
		v.Version = strings.TrimPrefix(info.Main.Version, "v") // a tag, from go install …@vX.Y.Z
	}
	if v.Version == "" || v.Version == "(devel)" {
		v.Version = "dev"
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			v.Commit = s.Value
		case "vcs.modified":
			v.Modified = s.Value == "true"
		}
	}
	return v
}

// String is `aid 0.1.0 (aed0a05, modified)`.
func (v buildVersion) String() string {
	s := "aid " + v.Version
	if v.Commit != "" {
		short := v.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		if v.Modified {
			short += ", modified"
		}
		s += " (" + short + ")"
	}
	return s
}

func versionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print aid's version",
		Long: "Print the version this aid was built as, from package.json, with the git commit and whether the " +
			"tree had uncommitted changes when Go recorded them. A build without a version prints `dev`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v := currentVersion()
			return a.emit(v, fmt.Sprint(v))
		},
	}
}
