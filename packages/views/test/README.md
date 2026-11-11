Views CI keeps two isolated Vitest shards. `balanced-sequencer.ts` assigns files
by estimated total cost (environment, setup, imports, tests and hooks), placing
the longest files on the lightest shard first. Each shard still uses Vitest's
default ordering within its assigned files.

All runners use the same committed `shard-timings.json`, never a runner's local
result cache. Discovery order and checkout paths do not affect assignment.
New or renamed files are included using a file-size estimate; removed files in
the snapshot are ignored. The snapshot affects scheduling only, not coverage.

To refresh the snapshot after substantial test changes, run the whole suite
from the repository root on an otherwise idle machine:

```sh
pnpm --filter @multica/views exec vitest run --maxWorkers=3 --no-cache --reporter=default --reporter=./test/timing-reporter.ts
```

The reporter only writes a snapshot after a successful, unsharded run. Review
and commit it with the change. Ordinary test runs and CI do not rewrite it.
The initial snapshot was measured locally on Node 24.11.1, macOS arm64, with
three workers after introducing the lazy calendar. Absolute times vary across
machines; these values are weights, not a CI performance promise.
