# JavaScript and TypeScript

Inspect the nearest `package.json`, the workspace manifest, the lockfile, and
the runner config. Use the owning package's package-relative test path when a
workspace command changes the runner's working directory.

For pnpm plus Vitest, when repository configuration does not provide a
dedicated command, use this direct runner shape from the repository root:

```text
["pnpm", "--filter", "<workspace>", "exec", "vitest", "run", "<package-relative-test-file>"]
```

Do not use this shape for a focused run:

```text
["pnpm", "--filter", "<workspace>", "test", "--", "<test-file>"]
```

The package-script layer can forward the separator in a way the runner does not
interpret as a file filter, expanding discovery to the whole package.

For other package managers or runners, inspect the repository script and local
runner help. Do not translate the pnpm/Vitest argv mechanically.

If the installed Vitest version exposes a list command, use the same file
filter with that command first and require exactly one discovered test file.
