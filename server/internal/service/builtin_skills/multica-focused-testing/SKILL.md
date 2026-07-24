---
name: multica-focused-testing
description: "Use when selecting or running a focused or targeted test in any user repository. Detect the repository's stack and runner first, prefer its commands, and verify discovery before execution. Not for an explicitly requested full-suite run."
---

# Focused testing

Run the narrowest runner-native scope without expanding it to a full suite.

## Workflow

1. Read repository instructions and test configuration. Verify the target,
   ownership, build tool, and runner.
2. Prefer repository Agent configuration, then a dedicated repository script,
   then a runner-native focused selector confirmed by configuration or local
   `--help`. Infer only as a last resort.
3. Keep the target as a distinct argument. Do not guess whether a wrapper
   forwards separators or positional arguments, and do not add a separator
   unless the detected tool requires it.
4. Use list, collect, discovery, or dry-run mode first when available. Compare
   it with the runner-native scope: file, package plus case, target, class,
   module, or project. Never impose
   `expected_file_count=1` on a runner that does not discover by file.
5. Run only when scope is confirmed. If discovery is broader or the target,
   ownership, runner, or forwarding contract remains unclear, stop and correct
   or report the gap rather than guessing.

## Stack reference

After detection, read exactly the matching reference:

- JavaScript or TypeScript: `references/javascript-typescript.md`
- Go: `references/go.md`
- Python: `references/python.md`
- Rust: `references/rust.md`
- JVM (Gradle or Maven): `references/jvm.md`
- .NET: `references/dotnet.md`
- Ruby: `references/ruby.md`

If no reference matches, use the repository's own instructions and the detected
runner's local help. Do not borrow a template from another stack.
