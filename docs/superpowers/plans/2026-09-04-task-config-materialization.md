# Plan: controlled task configuration materialization

> **For implementation:** follow this plan in the current task, preserving the red/green checkpoints below.

**Goal:** Add a server-side `task_config` provider contract and a daemon-side, fail-closed atomic materializer for `backend.hcl`, without changing `local_directory` or agent `custom_env`.

**Design:** Reuse polymorphic `project_resource` rows and claim payloads. Add a typed `task_config` validator, an authenticated daemon resolve endpoint backed by an injectable Secrets Manager provider, and a daemon client/materializer. Sidecar manifest helpers reserve cleanup paths before writes; an in-memory attestation prevents an untrusted/pre-existing file from satisfying preflight.

**Verification:** TDD unit/contract tests first, then focused daemon/handler tests, race and vet checks for affected packages, and repository checks available without a control-plane deployment.

## Steps

1. Add failing resource/provider contract tests for schema validation, version immutability, identity/selector refusal, raw-byte response, and secret-free serialization.
2. Implement the server resource validator, immutable-version update guard, provider interface/AWS adapter, resolve endpoint/route, and daemon claim/client wire contract.
3. Add failing daemon materializer/preflight tests for path traversal, symlinks, collisions, mode, atomic writes, tuple/source/ownership gates, and fake-runner call counts.
4. Implement sidecar reservation/targeted cleanup and daemon materializer integration before `StartTask`; keep config bytes out of `TaskResult`, metadata, env, and logs.
5. Add lifecycle/restart/concurrency/leak regression tests and wire startup/orphan recovery to clean manifest-owned config remnants.
6. Run gofmt, focused tests, race/vet checks, inspect the diff, commit, push, and open a PR titled with `AERIS-1308` (without a closing keyword).
