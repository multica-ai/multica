-- Platform side of the knowledge base (plan 10.11, 10.13, 10.14): model and base
-- settings, restricted-base access, the WeKnora login of each owner, and the
-- tenant deprovision queue.

-- name: SetKnowledgeBaseLLMSettings :execrows
-- Records the switches Multica asked WeKnora to apply, and marks the base's
-- defaults as written so a later enable leaves an admin's choice alone.
UPDATE knowledge_base
SET wiki_enabled = @wiki_enabled::boolean,
    summaries_enabled = @summaries_enabled::boolean,
    llm_defaults_applied = true
WHERE id = @id AND workspace_id = @workspace_id;

-- name: CountWikiEnabledKnowledgeBases :one
-- Wiki is limited to three bases (spec 3.3). The base being changed is left out
-- of the count so turning a base back on does not count against itself.
SELECT count(*)::bigint FROM knowledge_base
WHERE workspace_id = @workspace_id AND archived_at IS NULL AND wiki_enabled
  AND id <> @id;

-- name: SetKnowledgeBaseVisibility :execrows
UPDATE knowledge_base SET visibility = @visibility::text
WHERE id = @id AND workspace_id = @workspace_id;

-- name: GetWorkspaceKnowledgeTenant :one
SELECT weknora_tenant_id FROM workspace_knowledge WHERE workspace_id = @workspace_id;

-- name: InsertKnowledgeOwnerAccountPending :execrows
-- The sealed password is written before WeKnora is asked for the login, so a
-- crash in between leaves a password a retry can use.
INSERT INTO knowledge_owner_account (workspace_id, user_id, weknora_user_id, email, password_ciphertext)
VALUES (@workspace_id, @user_id, '', @email, @password_ciphertext)
ON CONFLICT (workspace_id, user_id) DO NOTHING;

-- name: GetKnowledgeOwnerAccount :one
SELECT * FROM knowledge_owner_account WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: SetKnowledgeOwnerWeKnoraUser :execrows
UPDATE knowledge_owner_account SET weknora_user_id = @weknora_user_id
WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: ClearKnowledgeOwnerPassword :execrows
-- Shows the password once: the ciphertext is cleared by the reveal that reads it.
UPDATE knowledge_owner_account
SET password_ciphertext = NULL, revealed_at = now()
WHERE workspace_id = @workspace_id AND user_id = @user_id AND password_ciphertext IS NOT NULL;

-- name: DeleteWorkspaceKnowledgeOwnerAccounts :exec
DELETE FROM knowledge_owner_account WHERE workspace_id = @workspace_id;

-- name: DeleteWorkspaceKnowledgeACL :exec
DELETE FROM knowledge_acl WHERE workspace_id = @workspace_id;

-- name: DeleteWorkspaceKnowledgeRepoFiles :exec
DELETE FROM knowledge_repo_file WHERE workspace_id = @workspace_id;

-- name: DeleteWorkspaceKnowledgeRepoSync :exec
DELETE FROM knowledge_repo_sync WHERE workspace_id = @workspace_id;

-- name: QueueKnowledgeTenantDeprovision :execrows
-- Written inside the workspace delete transaction: the tenant is queued only if
-- the delete commits. A repeated delete finds the row and changes nothing.
INSERT INTO knowledge_tenant_deprovision (workspace_id, weknora_tenant_id, state, hold_until, next_attempt_at)
SELECT wk.workspace_id, wk.weknora_tenant_id, 'held', @hold_until::timestamptz, @hold_until::timestamptz
FROM workspace_knowledge wk
WHERE wk.workspace_id = @workspace_id
ON CONFLICT (workspace_id) DO NOTHING;

-- name: ClaimDueKnowledgeDeprovisions :many
-- Claims rows whose hold has passed and whose next attempt is due. A run that
-- died while holding a claim is reclaimed after fifteen minutes.
UPDATE knowledge_tenant_deprovision
SET state = 'running', claimed_at = now(), attempts = attempts + 1
WHERE workspace_id IN (
    SELECT d.workspace_id FROM knowledge_tenant_deprovision d
    WHERE d.hold_until <= now()
      AND d.next_attempt_at <= now()
      AND (d.state = 'held' OR (d.state = 'running' AND d.claimed_at < now() - interval '15 minutes'))
    ORDER BY d.next_attempt_at ASC
    LIMIT @batch_size::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkKnowledgeDeprovisionDone :execrows
UPDATE knowledge_tenant_deprovision
SET state = 'done', completed_at = now(), last_error = ''
WHERE workspace_id = @workspace_id AND state = 'running';

-- name: RetryKnowledgeDeprovision :execrows
UPDATE knowledge_tenant_deprovision
SET state = 'held', next_attempt_at = @next_attempt_at::timestamptz, last_error = @last_error::text, claimed_at = NULL
WHERE workspace_id = @workspace_id AND state = 'running';

-- name: CancelKnowledgeDeprovision :execrows
-- Cancels a purge that has not run. Only a held row can be cancelled; the
-- tenant is then kept.
DELETE FROM knowledge_tenant_deprovision
WHERE workspace_id = @workspace_id AND state = 'held';

-- name: GetKnowledgeDeprovision :one
SELECT * FROM knowledge_tenant_deprovision WHERE workspace_id = @workspace_id;

-- name: ListKnowledgeACLForBase :many
SELECT * FROM knowledge_acl
WHERE workspace_id = @workspace_id AND knowledge_base_id = @knowledge_base_id
ORDER BY subject_type ASC, subject_id ASC;

-- name: UpsertKnowledgeACL :exec
INSERT INTO knowledge_acl (workspace_id, knowledge_base_id, subject_type, subject_id, access)
VALUES (@workspace_id, @knowledge_base_id, @subject_type, @subject_id, @access)
ON CONFLICT (knowledge_base_id, subject_type, subject_id)
DO UPDATE SET access = EXCLUDED.access, updated_at = now();

-- name: DeleteKnowledgeACL :execrows
DELETE FROM knowledge_acl
WHERE workspace_id = @workspace_id AND knowledge_base_id = @knowledge_base_id
  AND subject_type = @subject_type AND subject_id = @subject_id;

-- name: ListKnowledgeACLForAgent :many
-- Every grant that reaches an agent: its own, its squads' and its squads'
-- floors'. Archived squads grant nothing.
SELECT a.knowledge_base_id, a.access FROM knowledge_acl a
WHERE a.workspace_id = @workspace_id AND (
    (a.subject_type = 'agent' AND a.subject_id = @agent_id)
 OR (a.subject_type = 'squad' AND a.subject_id IN (
        SELECT sm.squad_id FROM squad_member sm
        JOIN squad s ON s.id = sm.squad_id
        WHERE sm.member_type = 'agent' AND sm.member_id = @agent_id AND s.archived_at IS NULL))
 OR (a.subject_type = 'floor' AND a.subject_id IN (
        SELECT s.floor_id FROM squad_member sm
        JOIN squad s ON s.id = sm.squad_id
        WHERE sm.member_type = 'agent' AND sm.member_id = @agent_id AND s.archived_at IS NULL
          AND s.floor_id IS NOT NULL))
);

-- name: ListLiveAgentIDsForKnowledge :many
SELECT id FROM agent
WHERE workspace_id = @workspace_id AND archived_at IS NULL AND kind = 'user';

-- name: KnowledgeACLSquadExists :one
SELECT EXISTS (SELECT 1 FROM squad WHERE id = @id AND workspace_id = @workspace_id AND archived_at IS NULL);

-- name: KnowledgeACLFloorExists :one
SELECT EXISTS (SELECT 1 FROM office_floor WHERE id = @id AND workspace_id = @workspace_id);

-- name: KnowledgeACLAgentExists :one
SELECT EXISTS (SELECT 1 FROM agent WHERE id = @id AND workspace_id = @workspace_id AND archived_at IS NULL);

-- name: ListLiveAgentsForKnowledgeMember :many
-- Agents of a squad, for recomputing their access after a membership change.
SELECT sm.member_id FROM squad_member sm
WHERE sm.squad_id = @squad_id AND sm.member_type = 'agent';
