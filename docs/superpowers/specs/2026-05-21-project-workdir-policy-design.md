# Project Workdir Policy Design

## Decision

Add a project-level advisory workdir policy:

- `workdir_policy`: `none` or `advisory`
- `canonical_workdir`: optional local path string

This is intentionally advisory. The daemon injects the policy into the agent context and writes it to `.multica/project/resources.json`, but the platform does not claim hard filesystem enforcement until every runtime has a tool-level sandbox/enforcement mechanism.

## Runtime Behavior

When an issue belongs to a project, the claim response includes the project's workdir policy and canonical workdir. The daemon passes those fields into `execenv.TaskContextForEnv`.

The runtime config tells agents:

- the preferred local path, when configured
- that the policy is advisory
- to keep repo checkouts and generated files under that path when practical
- not to treat the instruction as a security boundary

## API Behavior

Project create/update accept optional:

- `workdir_policy`
- `canonical_workdir`

Validation:

- unknown policy returns `400`
- empty canonical path is stored as `null`
- canonical path is trimmed
- control characters are rejected
- when a canonical path is provided and policy is omitted, policy defaults to `advisory`

## UI Behavior

Project detail exposes a compact Workdir property in the existing properties block. Copy must say "preferred" and "advisory" so the product does not overpromise sandboxing.

## Follow-Up

Hard enforcement should be a separate milestone per runtime/provider because Codex, Claude, OpenCode, OpenClaw, and others expose different workspace-root controls.
