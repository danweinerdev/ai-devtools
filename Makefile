# go.mod pins the Go version; let go fetch it when the installed one is older
# (some distros set GOTOOLCHAIN=local). An explicit GOTOOLCHAIN still wins.
export GOTOOLCHAIN ?= auto

.PHONY: build check lint test test-go test-opencode test-opencode-e2e test-release release install-aid install-codex uninstall-codex bump-patch bump-minor bump-major

# Regenerate every harness artifact from skills/ and agents/.
build:
	@go run ./tools/plugin-compiler

# THE gate: frontmatter + cross-reference validation, generated files match
# sources, lint is clean, and every test passes.
check: lint test-go test-opencode test-release
	@go run ./tools/plugin-compiler --check

# Go lint: formatting and staticcheck, pinned and run through go run so
# nothing needs installing. CI also runs actionlint.
STATICCHECK = honnef.co/go/tools/cmd/staticcheck@v0.8.2

lint:
	@test -z "$$(gofmt -l cmd internal tools)" || { echo "gofmt: unformatted files:"; gofmt -l cmd internal tools; exit 1; }
	@go run $(STATICCHECK) ./...

# Go tooling: vet and unit tests (the plugin compiler, the aid tool, and its
# Claude guardrail hooks, including a smoke run of the wired hooks.json).
test-go:
	@go vet ./...
	@go test ./...

test: check

# The OpenCode 2 plugin's offline tests (node's built-in runner, no
# dependencies). Skipped with a notice where node isn't installed; CI has it.
test-opencode:
	@if command -v node >/dev/null 2>&1; then \
		node --test ".opencode-plugin/test/*.test.js"; \
	else \
		echo "test-opencode: skipped, node is not installed"; \
	fi

# The OpenCode 2 live suite: real model calls against a private OpenCode
# server, driven by the aid.json the plugin uses (or AID_E2E_CONFIG=path/to/aid.json). Local
# only; never part of check or CI. See .opencode-plugin/live/README.md.
test-opencode-e2e:
	@node .opencode-plugin/live/e2e.js $(E2E_ARGS)

# The release flow's tests: bump-version.py driven end to end in throwaway git
# repos, so committing and tagging are exercised without cutting a release
# here. Skipped with a notice where python3 isn't installed (the bump targets
# need it too); CI has it.
test-release:
	@if command -v python3 >/dev/null 2>&1; then \
		python3 -m unittest discover -s tests -p "test_*.py"; \
	else \
		echo "test-release: skipped, python3 is not installed"; \
	fi

# Release builds of aid: build/<os>-<arch>/aid (aid.exe on Windows). Static
# (no cgo), stripped of symbols and DWARF, and without local paths. Go has no
# LTO switch: its linker already sees the whole program and drops dead code.
# Override the set with RELEASE_PLATFORMS="linux/amd64 darwin/arm64".
# aid reports the plugin version from package.json (`aid version`); bump-version.py
# keeps it in step with the other two manifests.
AID_VERSION := $(shell sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' package.json | head -n 1)
VERSION_LDFLAG := -X main.version=$(AID_VERSION)

RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

release:
	@rm -rf build
	@for p in $(RELEASE_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		out=build/$$os-$$arch/aid$$ext; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags='-s -w $(VERSION_LDFLAG)' -o $$out ./cmd/aid || exit 1; \
		echo "$$out"; \
	done

# Install the aid tool (task graph and state record) into GOBIN, or GOPATH/bin.
install-aid:
	@go install -ldflags='$(VERSION_LDFLAG)' ./cmd/aid

# Symlink generated agents into Codex's agent directory (skills need no install;
# the OpenCode plugin registers its agents itself).
install-codex: check
	@scripts/install.sh codex

uninstall-codex:
	@scripts/install.sh codex --uninstall

# Version bumps. bump-version.py moves the version in all three manifests, runs
# `make build` and `make check`, then commits vX.Y.Z and creates the annotated
# tag. The whole suite runs inside it before anything is written, so no gate is
# duplicated here. Never pushes; see AGENTS.md, Versioning.
bump-patch:
	@python3 bump-version.py patch

bump-minor:
	@python3 bump-version.py minor

bump-major:
	@python3 bump-version.py major
