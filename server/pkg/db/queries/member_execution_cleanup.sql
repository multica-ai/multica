-- name: IsMemberExecutionRuntimeRevoked :one
SELECT EXISTS (
    SELECT 1
    FROM agent_runtime
    WHERE id = $1
      AND metadata @> '{"member_execution_revoked": true}'::jsonb
);

-- name: AssertMemberExecutionRuntimeActive :one
-- The row lock serializes claim finalization with member cleanup's
-- ForceOfflineRuntimesByIDs update. Either the task token commits first and
-- cleanup immediately cancels/deletes it, or cleanup wins and this returns no
-- row so no execution payload can be finalized afterward.
SELECT id
FROM agent_runtime
WHERE id = $1
  AND NOT (metadata @> '{"member_execution_revoked": true}'::jsonb)
FOR SHARE;
