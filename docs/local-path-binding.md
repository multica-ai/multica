# Local Path Binding (MVP)

This document describes the current MVP behavior for binding workspace/project entities to local folders.

## Scope

- `workspace.local_path` is optional and stored in the workspace row.
- `project.local_path` is optional and stored in the project row.
- The feature is designed for single-machine/self-hosted usage.

## Constraints

- `local_path` must be an absolute path.
- The backend trims and normalizes `local_path` before storing it.
- Control characters are rejected.
- Empty/blank values are rejected on create; update supports explicit clear via `null`.
- Maximum normalized path length is 1024 characters.

## API Semantics

- Create:
  - `POST /api/workspaces` accepts optional `local_path`.
  - `POST /api/projects` accepts optional `local_path`.
- Update:
  - Omitting `local_path` preserves existing value.
  - Sending `"local_path": null` clears existing value.
  - Sending a non-empty string updates the value after normalization.

## UI Behavior

- Workspace creation forms expose `Create from existing folder`.
- Project creation modal exposes `Create from existing folder`.
- When enabled, folder path is required and trimmed before submission.

## Operational Notes

- The path must be valid on the machine where daemon/runtime actually executes tasks.
- In multi-device teams the same absolute path may not exist on other machines.
- Device-specific path mapping is out of scope for this MVP and should be introduced as a follow-up feature.
