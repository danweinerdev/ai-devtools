# Finding the checks

A task's checks are the project's own commands, taken from what the repository already says about how it is tested and linted. Read in this order and stop at the first evidence that settles the command:

1. **Test configuration.** A runner's own config names the runner, its test paths, and often its command: `pytest.ini` or `[tool.pytest.ini_options]`, `jest.config.*`, `vitest.config.*`, `.mocharc.*`, `.rspec`, `CMakeLists.txt` with CTest, a Gradle or Maven test task.
2. **Dependency manifests.** A declared test or lint dependency is strong evidence of intent: `pyproject.toml`, `requirements*.txt`, `package.json`, `go.mod`, `Cargo.toml`, `pom.xml`, `build.gradle(.kts)`, `*.csproj`, `Gemfile`. A lockfile also says which package manager runs the scripts (`package-lock.json` → npm, `pnpm-lock.yaml` → pnpm, `yarn.lock` → yarn, `bun.lock*` → bun).
3. **Build system.** A `Makefile`, `justfile`, `tox.ini`, CI workflow, or build script that already runs the tests shows the command the project trusts, including any flags or working directory it requires.
4. **Imports.** Test and source imports (`import pytest`, `import { describe } from "vitest"`, `"testing"`, `#[cfg(test)]`, `org.junit`, `Xunit`) confirm a runner when the files above are missing or silent. Imports alone are the weakest evidence: they show what a file uses, not how the project runs it.

**Ask, don't guess.** When the evidence doesn't settle a command — two runners configured, a script that calls something unrecognisable, no test configuration at all, or a manifest that lists a framework nobody invokes — ask the user. Don't fall back to a language default, don't turn a low-confidence import into a command, and don't invent flags. Record the answer as the check; if the question blocks the plan, ask it now (aid-think-clarify), otherwise it waits in the state record (aid-state).

## Test commands

| Marker (evidence) | Run the tests | Narrow to one test |
|---|---|---|
| `pytest.ini`, `pyproject.toml` `[tool.pytest.ini_options]`, `tox.ini`, pytest dependency | `python -m pytest` | `python -m pytest tests/test_foo.py::test_name` |
| `unittest` classes, no pytest configuration or dependency | `python -m unittest discover` | `python -m unittest tests.test_foo.TestClass.test_name` |
| `package.json` `scripts.test`, `jest.config.*`, jest dependency | `npm test` (or the lockfile's package manager) or `npx jest` | `npx jest path/to/foo.test.js -t "test name"` |
| `vitest.config.*`, vitest dependency | `npx vitest run` | `npx vitest run path/to/foo.test.ts -t "test name"` |
| `.mocharc.*`, mocha dependency | `npx mocha` | `npx mocha path/to/foo.test.js --grep "test name"` |
| `go.mod` | `go test ./...` | `go test ./pkg/name -run '^TestName$'` |
| `Cargo.toml` | `cargo test` | `cargo test test_name`, or `cargo test --test integration_name` |
| `pom.xml` | `mvn test` | `mvn -Dtest=ClassName#method test` |
| `build.gradle`, `build.gradle.kts`, `./gradlew` | `./gradlew test` | `./gradlew test --tests 'com.example.ClassName.method'` |
| `*.csproj`, `*.sln` | `dotnet test` | `dotnet test --filter "FullyQualifiedName~Namespace.ClassName.Method"` |
| `CMakeLists.txt` with CTest | `ctest --test-dir <build-dir> --output-on-failure` | `ctest --test-dir <build-dir> -R '^TestName$' --output-on-failure` |
| `Gemfile` with RSpec, `.rspec` | `bundle exec rspec` | `bundle exec rspec spec/foo_spec.rb:42` |
| `Gemfile` with Minitest, `test/` | `bundle exec rake test` | `ruby -Itest test/foo_test.rb -n test_name` |

If the check needs a build first, put the build in the same check with `&&`: `cmake --build build && ctest --test-dir build --output-on-failure`. Keep it non-interactive.

## Lint and type checks

Use only the check-only form. A lint command that rewrites files is not a check: it can turn a failure into a pass and leave an edit the brief didn't ask for.

| Tool | Check command |
|---|---|
| ruff | `ruff check .` (never `ruff check --fix`) |
| flake8 | `flake8 .` |
| mypy | `mypy .` |
| ESLint | `npx eslint .` (never `--fix`) |
| TypeScript | `npx tsc --noEmit` |
| go vet | `go vet ./...` |
| staticcheck | `staticcheck ./...` when the project configures or runs it |
| Clippy | `cargo clippy` (never `--fix`); add `-- -D warnings` only when the project's config or CI already does |
| Maven checks | the task the build configures, e.g. `mvn -q checkstyle:check` or `mvn verify` |
| Gradle checks | `./gradlew check` |
| .NET build/format | `dotnet build`; `dotnet format --verify-no-changes` when the project configures it |
| C/C++ compiler warnings | the project's configured build with its warning flags; `clang-tidy` when configured |
| RuboCop | `bundle exec rubocop` (never `-a` or `-A`) |

Never add `|| true`, a pipe that hides the exit code, `--watch`, or an interactive prompt. If the project's lint command writes files, record the check-only variant or ask.

## One named check where practical

The task graph runs each check for ten minutes and keeps it attached to the task for the whole feature, so prefer the test written for this task over the whole suite: one file, one test name. The full rules, including why `.aid/tasks.json` is code, are in aid-graph ([../aid-graph/SKILL.md](../../aid-graph/SKILL.md)).
