-- name: ListWorktreeResourcesForDaemon :many
SELECT * FROM project_resource
WHERE workspace_id = @workspace_id AND resource_type = 'local_directory'
 AND resource_ref->>'daemon_id' = @daemon_id::text
 AND resource_ref->>'execution_mode' = 'worktree';

-- name: ListWorktreeReadinessForProject :many
SELECT r.* FROM local_worktree_readiness r
JOIN project_resource pr ON pr.id = r.resource_id AND pr.resource_ref = r.resource_ref
WHERE pr.project_id = @project_id AND pr.workspace_id = @workspace_id;

-- name: RecordWorktreeReadiness :execrows
INSERT INTO local_worktree_readiness (resource_id, resource_ref, checked_at, expires_at, status, measurement)
SELECT pr.id, pr.resource_ref, now(), now() + interval '90 seconds', @status, @measurement
FROM project_resource pr
WHERE pr.id = @resource_id AND pr.workspace_id = @workspace_id
 AND pr.resource_type = 'local_directory' AND pr.resource_ref->>'execution_mode' = 'worktree'
 AND pr.resource_ref->>'daemon_id' = @daemon_id::text AND pr.resource_ref = @resource_ref::jsonb
ON CONFLICT (resource_id) DO UPDATE SET
 resource_ref = EXCLUDED.resource_ref, checked_at = EXCLUDED.checked_at,
 expires_at = EXCLUDED.expires_at, status = EXCLUDED.status, measurement = EXCLUDED.measurement;

-- name: ListWorktreeReadinessRuntimeIDs :many
SELECT id FROM agent_runtime WHERE workspace_id = @workspace_id AND daemon_id = @daemon_id::text;
