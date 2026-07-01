# NanoClaw integration

This integration lets a NanoClaw agent create Multica issues for an agent or
squad by display name and read an issue's current status.

The Multica PAT stays in the host-side `multica` CLI configuration. NanoClaw
receives a separate bridge token whose authority is limited to two bridge
operations:

- `POST /v1/issues`
- `GET /v1/issues/{id}`

Install the NanoClaw side by copying `add-multica` into the target NanoClaw
checkout as `.claude/skills/add-multica`, then run `/add-multica` there.

See [`add-multica/SKILL.md`](add-multica/SKILL.md) for setup and verification.
