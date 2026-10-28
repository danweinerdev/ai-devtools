# go.mod pins the Go version; let go fetch it when the installed one is older
# (some distros set GOTOOLCHAIN=local). An explicit GOTOOLCHAIN still wins.
export GOTOOLCHAIN ?= auto

.PHONY: build check test test-go test-claude release install-aid install-codex install-opencode uninstall-codex uninstall-opencode

# Regenerate every harness artifact from skills/ and agents/.
build:
	@go run ./tools/plugin-compiler

# THE gate: frontmatter + cross-reference validation, generated files match
# sources, and every test passes.
check: test-go test-claude
	@go run ./tools/plugin-compiler --check

# Go tooling: vet and unit tests (the plugin compiler and the aid tool).
test-go:
	@go vet ./...
	@go test ./...

# Claude guardrail hooks: unit tests plus a smoke run of the wired hooks.
test-claude:
	@python3 -m unittest discover -s harness/claude/tests -t harness/claude/tests
	@python3 harness/claude/scripts/smoke_hooks.py

test: check

# Release builds of aid: build/<os>-<arch>/aid (aid.exe on Windows). Static
# (no cgo), stripped of symbols and DWARF, and without local paths. Go has no
# LTO switch: its linker already sees the whole program and drops dead code.
# Override the set with RELEASE_PLATFORMS="linux/amd64 darwin/arm64".
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

release:
	@rm -rf build
	@for p in $(RELEASE_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		out=build/$$os-$$arch/aid$$ext; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags='-s -w' -o $$out ./cmd/aid || exit 1; \
		echo "$$out"; \
	done

# Install the aid tool (task graph and state record) into GOBIN, or GOPATH/bin.
install-aid:
	@go install ./cmd/aid

# Symlink generated agents into the harness's agent directory (skills need no install).
install-codex: check
	@scripts/install.sh codex

install-opencode: check
	@scripts/install.sh opencode

uninstall-codex:
	@scripts/install.sh codex --uninstall

uninstall-opencode:
	@scripts/install.sh opencode --uninstall
