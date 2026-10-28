package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	for _, c := range []struct {
		v    buildVersion
		want string
	}{
		{buildVersion{Version: "0.1.0"}, "aid 0.1.0"},
		{buildVersion{Version: "0.1.0", Commit: "aed0a05b408b87d65b6924c7331fb4c7f6d4e7b4"}, "aid 0.1.0 (aed0a05)"},
		{buildVersion{Version: "dev", Commit: "abc", Modified: true}, "aid dev (abc, modified)"},
	} {
		if got := c.v.String(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.v, got, c.want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	r := newRepo(t)
	out := r.ok("version")
	if !strings.HasPrefix(out, "aid ") {
		t.Fatalf("aid version: %q", out)
	}
	if flag := r.ok("--version"); flag != out {
		t.Fatalf("--version %q differs from version %q", flag, out)
	}
	var v buildVersion
	if err := json.Unmarshal([]byte(r.ok("version", "--json")), &v); err != nil || v.Version == "" {
		t.Fatalf("version --json: %+v %v", v, err)
	}
}
