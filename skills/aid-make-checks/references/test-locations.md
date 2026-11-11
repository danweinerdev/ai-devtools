# Where tests live

The project's existing tests are the first evidence: find tests of the same kind and put the new one beside them, using the same directory shape and file naming. A runner's configured path wins over the language convention.

| Language | Conventional location | Naming |
|---|---|---|
| Python | `tests/` at the project root, mirroring the package directories; a configured `testpaths` wins | `test_*.py` or `*_test.py` |
| JavaScript / TypeScript | `__tests__/`, `tests/`, or beside the source; the runner's `testMatch` wins | `*.test.*` or `*.spec.*` |
| Go | the same package directory as the code under test | `*_test.go`; fixtures in `testdata/` |
| Rust | unit tests in `#[cfg(test)] mod tests` in the source file; integration tests as top-level files in `tests/` (or `tests/<name>/main.rs`); shared helpers in `tests/common/mod.rs` | any name under `tests/` |
| Java | `src/test/java/...`, mirroring the main package path | `*Test.java` or `Test*.java` |
| Kotlin | `src/test/kotlin/...`, mirroring the main package path | `*Test.kt` |
| C# | a sibling test project such as `<Project>.Tests/`, mirroring namespaces | `*Tests.cs` or `*Test.cs` |
| C / C++ | `tests/` or `test/`, registered with the build so CTest discovers it | `test_*.cpp`, `*_test.cpp`, or the framework's own convention |
| Ruby | `spec/` mirroring `lib/` for RSpec; `test/` for Minitest | `*_spec.rb`; `*_test.rb` |

Rules:

- **Never outside the project's test paths.** No scratch directory at the repository root, no hidden directory, no `/tmp`, no build output, no `.aid/`. A test the runner doesn't discover doesn't exist.
- **Mirror the layout.** Match the existing directory depth, package mirrors, helper and fixture locations, and naming. A new test that invents its own structure adds a second convention to maintain.
- **Register new test files.** Where discovery isn't automatic (CMake/CTest, a test task with an explicit list, a Gradle source set), the build change that registers the test belongs in the task's footprint.
- **Reuse the project's fixtures and helpers** rather than adding a parallel set in the test file.
