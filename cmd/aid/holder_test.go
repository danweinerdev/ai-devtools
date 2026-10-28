package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// aidEnv runs aid in r with extra environment variables.
func (r *repo) aidEnv(env []string, args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(aidBin, args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), env...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

func holderRepo(t *testing.T) *repo {
	r := newRepo(t)
	r.ok("init", "Feature")
	for _, id := range []string{"a", "b", "c"} {
		r.ok("add-task", "--id", id, "--title", "t", "--footprint", id+".go", "--result", "r", "--check", "true")
	}
	return r
}

func holderOfTask(t *testing.T, r *repo, id string) string {
	t.Helper()
	var shown []struct {
		State struct {
			Claim struct {
				Holder string `json:"holder"`
			} `json:"claim"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(r.ok("show", id, "--json")), &shown); err != nil || len(shown) != 1 {
		t.Fatalf("show %s: %v", id, err)
	}
	return shown[0].State.Claim.Holder
}

func TestHolderDefaultsToMain(t *testing.T) {
	r := holderRepo(t)
	r.ok("claim-task", "a")
	if got := holderOfTask(t, r, "a"); got != DefaultHolder {
		t.Fatalf("holder %q, want %q", got, DefaultHolder)
	}
	// The lone agent's whole loop needs no --holder.
	contains(t, r.ok("complete-task", "a"), "done a")
	r.ok("claim-task", "b")
	r.ok("block-task", "b", "--reason", "waiting")
	r.ok("release-task", "b")
	r.ok("claim-task", "b")
	r.ok("release-task", "b")
}

func TestHolderFromEnvironmentAndFlagWins(t *testing.T) {
	r := holderRepo(t)
	if _, errOut, code := r.aidEnv([]string{"AID_HOLDER=aid-mechanic"}, "claim-task", "a"); code != 0 {
		t.Fatal(errOut)
	}
	if got := holderOfTask(t, r, "a"); got != "aid-mechanic" {
		t.Fatalf("AID_HOLDER ignored: holder %q", got)
	}
	if _, errOut, code := r.aidEnv([]string{"AID_HOLDER=aid-mechanic"}, "claim-task", "b", "--holder", "aid-oracle"); code != 0 {
		t.Fatal(errOut)
	}
	if got := holderOfTask(t, r, "b"); got != "aid-oracle" {
		t.Fatalf("--holder should beat AID_HOLDER: holder %q", got)
	}
	// The env holder completes its own task without the flag.
	if _, errOut, code := r.aidEnv([]string{"AID_HOLDER=aid-mechanic"}, "complete-task", "a"); code != 0 {
		t.Fatal(errOut)
	}
}

func TestHolderDefaultStillExcludesOthers(t *testing.T) {
	r := holderRepo(t)
	r.ok("claim-task", "a") // held by main
	contains(t, r.refused("complete-task", "a", "--holder", "aid-oracle"), `held by "main"`)
	contains(t, r.refused("claim-task", "a", "--holder", "aid-oracle"), `held by "main"`)
	contains(t, r.refused("release-task", "a", "--holder", "aid-oracle"), "main")
	// And a worker's claim can't be taken over by the default holder.
	r.ok("claim-task", "b", "--holder", "aid-mechanic")
	contains(t, r.refused("complete-task", "b"), `held by "aid-mechanic"`)
	contains(t, r.refused("block-task", "b", "--reason", "x"), "aid-mechanic")
	if _, err := os.Stat(filepath.Join(r.dir, ".aid", "Feature", "tasks.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFinishRefusesAnEmptyGraph(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	contains(t, r.refused("finish"), "no tasks; the graph was never built")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
	r.ok("drop-task", "a", "--reason", "not needed")
	contains(t, r.ok("finish"), "finished: 0 done, 1 dropped")
}
