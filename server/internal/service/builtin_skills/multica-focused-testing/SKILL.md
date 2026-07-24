---
name: multica-focused-testing
description: "Use when selecting or running a focused or targeted test in any user repository — one file, package, module, class, or case — especially through package-manager or build-tool wrappers. Detect the repository's stack and runner first, prefer repository-defined commands, avoid guessing argument forwarding, and verify discovery scope before executing. Not for an explicitly requested full-suite run."
---

# Focused testing

Run the narrowest validation the repository and detected test runner actually
support without accidentally expanding it to a full suite.

## Workflow

1. Read repository instructions and test configuration before composing a
   command. A documented command or dedicated focused-test script is the source
   of truth.
2. Verify the requested target exists. Identify its owning workspace, package,
   module, crate, or project; then identify the package/build tool and test
   runner from repository files.
3. Choose the command in this order:
   - repository Agent configuration;
   - a dedicated repository script;
   - a runner-native focused selector confirmed by local configuration or
     `--help`;
   - inference only after the earlier sources are exhausted.
4. Keep the target as a distinct argument. Do not guess whether a wrapper
   forwards separators or positional arguments, and do not add a separator
   unless the detected tool requires it.
5. If the detected runner provides list, collect, discovery, or dry-run mode,
   use it before execution and compare the result with the expected scope.
6. Run the focused command. If output shows broader discovery than expected,
   stop and correct the command; do not treat that run as valid validation.

## Expected scope

Define scope in the runner's own terms. File-oriented runners may expect one
file; compiled backends may select a package plus one test name; integration
runners may select a test target or project. Never impose
`expected_file_count=1` on a stack whose runner does not discover tests by
individual file.

## Load only the detected stack

After inspecting the repository, read exactly the matching reference:

- JavaScript or TypeScript: `references/javascript-typescript.md`
- Go: `references/go.md`
- Python: `references/python.md`
- Rust: `references/rust.md`
- JVM (Gradle or Maven): `references/jvm.md`
- .NET: `references/dotnet.md`
- Ruby: `references/ruby.md`

If no reference matches, use the repository's own instructions and the detected
runner's local help. Do not borrow a template from another stack.

## Stop conditions

Do not execute a guessed command when the target is missing, module ownership
is ambiguous, the runner cannot be identified, or the only known command is a
broad suite with unverified forwarding. Inspect configuration or report the
validation gap instead.
