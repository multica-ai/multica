# Controlled task configuration materialization

## Context

NPR release tasks need `deploy/terraform/backend.hcl` without relying on an ignored file in a developer checkout. The control plane owns the provider reference and binding; the daemon is the only component that briefly holds provider bytes and writes the file.

## Contract

`task_config` is a project resource whose JSON reference contains an adapter name, provider reference, immutable version, relative destination, mode, and repo/target/account/region selectors. The claim payload carries only that reference. Writes are restricted to workspace `owner`/`admin` members and the provider reference must match the operator-configured `MULTICA_TASK_CONFIG_PROVIDER_REF_PREFIXES` allowlist; an empty allowlist disables new bindings. A task_config cannot coexist with a project-level `local_directory`, because the latter may place the task outside the daemon-owned env root. A daemon-only resolve endpoint checks the task, runtime, workspace, executor, project resource, approved provider reference, and selector tuple before returning `application/octet-stream` bytes. Provider errors use stable non-secret error bodies.

The first adapter is AWS Secrets Manager, injected behind a small provider interface. It uses the reference as `SecretId` and the version as `VersionId`; the secret value never enters JSON, logs, task metadata, environment variables, or the task result.

## Materialization

The daemon validates a single task config reference, resolves bytes, validates the destination against the prepared workdir, and rejects existing entries or symlink/non-directory parents. It reserves both the final path and a deterministic temporary sibling in the sidecar manifest before creating either file. The temporary file is chmod'ed `0600`, synced, closed, and renamed atomically. A returned in-memory attestation records task identity, source type, tuple, path, mode, and digest for the fail-closed preflight.

Preflight requires the attestation, `control_plane_managed`, exact tuple, a regular current-task-owned file, and mode `0600`. Only then does `runTask` call `StartTask` and launch the agent. Cleanup removes the config and temporary sibling synchronously on every materialization/agent lifecycle exit while preserving unrelated sidecars; the manifest remains the crash/restart recovery record.

## Security invariants

- Provider references and selectors are non-secret metadata; provider bytes are never serialized into a claim or error.
- All identity checks are server-side and task-scoped; cross-workspace, wrong-runtime, and selector mismatch requests fail closed.
- Destination paths are relative, inside the prepared workdir, non-symlinked, collision-free, and `0600`.
- Errors and logs expose only stable error codes and redacted presence/tuple fields.
