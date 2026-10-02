# Process-tree container regression

On Unix, `processtree` owns the direct child's wait and terminates the process
group. The system init owns reaping orphaned descendants. Linux containers
running the daemon must provide an external init/reaper; see
[the daemon container contract](../../../../CLI_AND_DAEMON.md#linux-containers).
No process-wide `waitpid(-1)` loop is used, so other callers retain ownership of
their direct-child exit statuses.

`TestPID1OrphanReaping` exercises the real runner with a shell that exits zero
and leaves a background `sleep` with redirected streams. It checks direct-child
exit statuses 0 and 7, then checks cleanup and `/proc` across three cycles. It is
opt-in because the negative control intentionally creates zombies until its
disposable PID namespace exits. Do not enable it on a long-lived PID 1 process.

Run these commands from the repository root with Docker available. Build a race
instrumented Linux test binary (Go and the test binary must use the same platform):

```sh
docker pull --platform linux/amd64 golang:1.26.6
docker run --rm --platform linux/amd64 --network none \
  -e GOTOOLCHAIN=local -e GOPROXY=off \
  -v "$PWD:/src" -w /src/server --entrypoint go \
  golang:1.26.6 test -race -c -o /src/processtree-pid1.test \
  ./internal/daemon/processtree
```

Negative control: run the binary directly as PID 1, without an init. This is an
unsupported deployment and is **expected to fail** after approximately 45 seconds
with cleanup errors and accumulating zombies. Do not use `go test` or a shell as
the entrypoint: the binary itself must be PID 1.

```sh
docker run --rm --platform linux/amd64 --network none \
  -e MULTICA_PID1_PROBE=1 -v "$PWD:/src:ro" \
  --entrypoint /src/processtree-pid1.test golang:1.26.6 \
  -test.run '^TestPID1OrphanReaping$' \
  -test.v -test.count=3 -test.timeout=70s
```

Supported deployment: use Docker's init as PID 1. This must pass all three
repetitions with no recorded descendants left after cleanup, correct direct-child
exit statuses, and the existing cancellation regression passing as well.

```sh
docker run --rm --init --platform linux/amd64 --network none \
  -e MULTICA_PID1_PROBE=1 -v "$PWD:/src:ro" \
  --entrypoint /src/processtree-pid1.test golang:1.26.6 \
  -test.run '^Test(PID1OrphanReaping|CombinedOutputKillsDescendantsHoldingOutput)$' \
  -test.v -test.count=3 -test.timeout=30s
```

This is an isolated runner regression, not a full daemon/task end-to-end test.
Without `MULTICA_PID1_PROBE=1`, normal package tests skip the container probe.
