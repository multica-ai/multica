# Rust

Inspect the workspace `Cargo.toml` and the target crate's manifest. Rust's
focused unit scope is usually a package plus a test name; an integration test
file is a named Cargo test target.

For an integration-test target:

```text
["cargo", "test", "-p", "<package>", "--test", "<integration-target>"]
```

List the libtest names in that target without running them:

```text
["cargo", "test", "-p", "<package>", "--test", "<integration-target>", "--", "--list"]
```

Then run one case using the full path printed by libtest:

```text
["cargo", "test", "-p", "<package>", "--test", "<integration-target>", "--", "<full-test-path>", "--exact"]
```

For a library unit test, replace `--test <integration-target>` with `--lib`.
Here the separator is required because the filter, `--list`, and `--exact`
belong to the compiled libtest harness, not Cargo. `--exact` matches only a full
path such as `module::tests::test_name`; a short function name can select zero
tests. Confirm the target name with Cargo metadata or the manifest instead of
deriving it only from a filesystem path.

These libtest arguments do not apply when the target declares
`harness = false`; use that target's own CLI.

## Official documentation

- Cargo package/target selection and test-argument forwarding:
  https://doc.rust-lang.org/cargo/commands/cargo-test.html
- libtest filters, `--list`, and `--exact`:
  https://doc.rust-lang.org/rustc/tests/
