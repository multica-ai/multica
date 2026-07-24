# Rust

Inspect the workspace `Cargo.toml` and the target crate's manifest. Rust's
focused unit scope is usually a package plus a test name; an integration test
file is a named Cargo test target.

For an integration-test target:

```text
["cargo", "test", "-p", "<package>", "--test", "<integration-target>"]
```

For an exact test case:

```text
["cargo", "test", "-p", "<package>", "<test-name>", "--", "--exact"]
```

Here the separator is required because `--exact` belongs to the compiled test
harness, not Cargo. Confirm the target name with Cargo metadata or the manifest
instead of deriving it only from a filesystem path.
