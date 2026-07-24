# Go

Go tests are compiled and selected by package and test name, not reliably by
running one `_test.go` file in isolation. Identify the owning module and package
from `go.mod` and the target path.

Use the package plus an anchored `-run` expression:

```text
["go", "test", "./path/to/package", "-run", "^TestName$", "-count=1"]
```

For a subtest, use an anchored parent/subtest expression supported by the
repository's Go version.

Do not pass a single `_test.go` file merely to imitate file-oriented runners:
that can omit package files, shared test helpers, or build-tag behavior. The
expected scope is the owning package with the requested test or benchmark
selected.
