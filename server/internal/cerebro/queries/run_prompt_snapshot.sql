-- FIR-3212: per-run production prompt snapshots (immutable — insert + read only).

-- CreateRunPromptSnapshot inserts the run's prompt evidence. ON CONFLICT
-- DO NOTHING makes daemon retries idempotent: the first write wins and a
-- replay can never overwrite recorded history (UPDATE is also blocked by
-- trigger). Returns 1 on first write, 0 on replay.
-- name: CreateRunPromptSnapshot :execrows
INSERT INTO cerebro_run_prompt_snapshot (
    workspace_id, task_id, agent_id, issue_id,
    agent_context_version, agent_context_version_id,
    provider, model, runtime_version, system_prompt_mode,
    layers, sha256_original, sha256_redacted, total_bytes, redacted
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (task_id) DO NOTHING;

-- name: GetRunPromptSnapshotByTask :one
SELECT * FROM cerebro_run_prompt_snapshot
WHERE task_id = $1;

-- ListRunPromptSnapshotsByAgent returns recent run snapshots for an agent,
-- newest first, without the heavy layers payload (list views + quality
-- aggregation per version need the refs, not the prompt bodies).
-- name: ListRunPromptSnapshotsByAgent :many
SELECT id, workspace_id, task_id, agent_id, issue_id,
       agent_context_version, agent_context_version_id,
       provider, model, runtime_version, system_prompt_mode,
       sha256_original, sha256_redacted, total_bytes, redacted, created_at
FROM cerebro_run_prompt_snapshot
WHERE agent_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- ResolveAgentContextVersionID finds the version row the daemon-reported
-- claim-time semver points at, so the snapshot can carry a stable FK.
-- name: ResolveAgentContextVersionID :one
SELECT id FROM agent_context_version
WHERE agent_id = $1 AND version = $2;
