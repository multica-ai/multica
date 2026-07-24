# JavaScript and TypeScript

Inspect the nearest `package.json`, the workspace manifest, the lockfile, and
the runner config. Use the owning package's package-relative test path when a
workspace command changes the runner's working directory.

For pnpm plus Vitest, when repository configuration does not provide a
dedicated command, use this direct runner shape from the repository root:

```text
["pnpm", "--filter", "<workspace>", "exec", "vitest", "run", "<package-relative-test-file>"]
```

`pnpm exec` runs the dependency command in the selected project's scope and
passes options after `exec` to that command. Vitest treats a positional value
as a substring filter over test-file paths, so pass the full package-relative
path and verify that it uniquely identifies the requested file.

Do not use this shape for a focused run:

```text
["pnpm", "--filter", "<workspace>", "test", "--", "<test-file>"]
```

pnpm passes arguments after the script name to the executed script. In this
shape Vitest receives the literal separator before the path; that is not the
direct file-filter argv above and can expand discovery to the whole package.

For other package managers or runners, inspect the repository script and local
runner help. Do not translate the pnpm/Vitest argv mechanically.

If the installed Vitest version exposes `list` and `--filesOnly`, verify the
same filter before execution:

```text
["pnpm", "--filter", "<workspace>", "exec", "vitest", "list", "<package-relative-test-file>", "--filesOnly"]
```

Require exactly one discovered test file.

## Official documentation

- pnpm filtering: https://pnpm.io/filtering
- pnpm exec and option forwarding: https://pnpm.io/cli/exec
- pnpm run argument forwarding: https://pnpm.io/cli/run
- Vitest CLI file filters, `run`, and `list`: https://vitest.dev/guide/cli
