# Go

Go tests are compiled and selected by package and test name, not reliably by
running one `_test.go` file in isolation. Identify the owning module and package
from `go.mod` and the target path.

Use the package plus an anchored `-run` expression:

```text
["go", "test", "./path/to/package", "-run", "^TestName$", "-count=1"]
```

For a top-level test, verify discovery without running it:

```text
["go", "test", "./path/to/package", "-list", "^TestName$"]
```

`-list` does not enumerate subtests. For a subtest, use an anchored
parent/subtest expression, such as `^TestName$/^SubtestName$`, after confirming
the exact names from source or verbose output. Go splits `-run` expressions at
unbracketed `/` characters and may run a matching parent to discover its
subtests.

Do not pass a single `_test.go` file merely to imitate file-oriented runners:
that can omit package files, shared test helpers, or build-tag behavior. The
expected scope is the owning package with the requested test selected.

## Official documentation

- Go testing flags (`-run`, `-list`, and `-count`):
  https://pkg.go.dev/cmd/go#hdr-Testing_flags
