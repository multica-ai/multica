package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type PostgresHookRepository struct {
	db                cerebrodb.DBTX
	managedMu         sync.Mutex
	managedWorkspaces map[string]struct{}
}

func NewPostgresHookRepository(db cerebrodb.DBTX) *PostgresHookRepository {
	return &PostgresHookRepository{db: db, managedWorkspaces: make(map[string]struct{})}
}

const hookPolicyColumns = `id, family_id, workspace_id, name, description, policy_version,
mode, fail_mode, condition_mode, event_types, conditions, baseline_at, published_at,
created_by_id, created_by_type, published_by_id, created_at, updated_at`

func qualifiedHookPolicyColumns(alias string) string {
	columns := strings.Split(hookPolicyColumns, ",")
	for index := range columns {
		columns[index] = alias + "." + strings.TrimSpace(columns[index])
	}
	return strings.Join(columns, ", ")
}

func (r *PostgresHookRepository) List(ctx context.Context, workspaceID string) ([]HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := r.ensureManagedPolicies(ctx, workspaceID, wsID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT id
		FROM cerebro_workflow_hook_family
		WHERE workspace_id=$1
		ORDER BY updated_at DESC`, wsID)
	if err != nil {
		return nil, err
	}
	var familyIDs []string
	for rows.Next() {
		var familyID pgtype.UUID
		if scanErr := rows.Scan(&familyID); scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		familyIDs = append(familyIDs, util.UUIDToString(familyID))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	policies := make([]HookPolicy, 0, len(familyIDs))
	for _, familyID := range familyIDs {
		policy, err := r.Get(ctx, workspaceID, familyID)
		if errors.Is(err, ErrHookPolicyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func (r *PostgresHookRepository) ListEffective(ctx context.Context, workspaceID string) ([]HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := r.ensureManagedPolicies(ctx, workspaceID, wsID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+qualifiedHookPolicyColumns("policy")+`
		FROM cerebro_workflow_hook_family family
		JOIN cerebro_workflow_hook_policy policy ON policy.id=family.active_policy_id
		WHERE family.workspace_id=$1 AND family.disabled=FALSE
		ORDER BY family.updated_at DESC`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var policies []HookPolicy
	for rows.Next() {
		policy, _, scanErr := scanHookPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if loadErr := r.loadPolicyParts(ctx, &policy); loadErr != nil {
			return nil, loadErr
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

// ensureManagedPolicies seeds the code-defined managed policies once per
// workspace per process. Seeding owns the definition (name, description,
// events, conditions) but NOT the operational state: mode and policy_version
// are set on first insert and never rewritten, so an owner who pauses or edits
// a managed policy keeps that decision across restarts.
func (r *PostgresHookRepository) ensureManagedPolicies(ctx context.Context, workspaceID string, wsID pgtype.UUID) error {
	r.managedMu.Lock()
	defer r.managedMu.Unlock()
	if _, ensured := r.managedWorkspaces[workspaceID]; ensured {
		return nil
	}

	var ownerID pgtype.UUID
	if err := r.db.QueryRow(ctx, `
		SELECT user_id
		FROM member
		WHERE workspace_id=$1
		ORDER BY
			CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
			created_at
		LIMIT 1
	`, wsID).Scan(&ownerID); err != nil {
		return fmt.Errorf("find managed hook policy owner: %w", err)
	}

	for _, definition := range managedHookPolicies(workspaceID) {
		policy := definition.Policy
		policyID, err := util.ParseUUID(policy.ID)
		if err != nil {
			return fmt.Errorf("parse managed hook policy %q: %w", definition.Key, err)
		}
		events, conditions, modifications, actions := managedPolicyJSON(policy)
		handler := policy.Handlers[0]
		if _, err := r.db.Exec(ctx, `
			INSERT INTO cerebro_workflow_hook_policy (
				id, family_id, workspace_id, name, description, policy_version,
				mode, fail_mode, event_types, conditions, published_at,
				created_by_id, created_by_type, published_by_id
			) VALUES ($1,$1,$2,$3,$4,1,'managed','closed',$5,$6,now(),$7,'member',$7)
			ON CONFLICT (id) DO UPDATE SET
				name=EXCLUDED.name,
				description=EXCLUDED.description,
				fail_mode='closed',
				event_types=EXCLUDED.event_types,
				conditions=EXCLUDED.conditions,
				published_at=COALESCE(cerebro_workflow_hook_policy.published_at, EXCLUDED.published_at),
				published_by_id=COALESCE(cerebro_workflow_hook_policy.published_by_id, EXCLUDED.published_by_id),
				updated_at=now()
			WHERE (
				cerebro_workflow_hook_policy.name,
				cerebro_workflow_hook_policy.description,
				cerebro_workflow_hook_policy.fail_mode,
				cerebro_workflow_hook_policy.event_types,
				cerebro_workflow_hook_policy.conditions
			) IS DISTINCT FROM (
				EXCLUDED.name,
				EXCLUDED.description,
				EXCLUDED.fail_mode,
				EXCLUDED.event_types,
				EXCLUDED.conditions
			)
		`, policyID, wsID, policy.Name, policy.Description, events, conditions, ownerID); err != nil {
			return fmt.Errorf("upsert managed hook policy %q: %w", definition.Key, err)
		}

		// A managed policy is its own family and is always the Live version:
		// it has no Draft and cannot be edited, so the pointer never moves.
		if _, err := r.db.Exec(ctx, `
			INSERT INTO cerebro_workflow_hook_family (id, workspace_id, active_policy_id)
			VALUES ($1,$2,$1)
			ON CONFLICT (id) DO UPDATE SET active_policy_id=EXCLUDED.active_policy_id, updated_at=now()
			WHERE cerebro_workflow_hook_family.active_policy_id IS DISTINCT FROM EXCLUDED.active_policy_id
		`, policyID, wsID); err != nil {
			return fmt.Errorf("upsert managed hook family %q: %w", definition.Key, err)
		}

		binding := policy.Bindings[0]
		if _, err := r.db.Exec(ctx, `
			INSERT INTO cerebro_workflow_hook_binding (policy_id, scope_kind, scope_id, priority)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (policy_id, scope_kind, scope_id) DO UPDATE SET priority=EXCLUDED.priority
			WHERE cerebro_workflow_hook_binding.priority IS DISTINCT FROM EXCLUDED.priority
		`, policyID, binding.Kind, binding.ID, binding.Priority); err != nil {
			return fmt.Errorf("upsert managed hook binding %q: %w", definition.Key, err)
		}
		if _, err := r.db.Exec(ctx, `
			INSERT INTO cerebro_workflow_hook_handler (
				id, policy_id, position, decision, requirement, modifications, actions
			) VALUES ($1,$2,0,$3,$4,$5,$6)
			ON CONFLICT (policy_id, position) DO UPDATE SET
				decision=EXCLUDED.decision,
				requirement=EXCLUDED.requirement,
				modifications=EXCLUDED.modifications,
				actions=EXCLUDED.actions
			WHERE (
				cerebro_workflow_hook_handler.decision,
				cerebro_workflow_hook_handler.requirement,
				cerebro_workflow_hook_handler.modifications,
				cerebro_workflow_hook_handler.actions
			) IS DISTINCT FROM (
				EXCLUDED.decision,
				EXCLUDED.requirement,
				EXCLUDED.modifications,
				EXCLUDED.actions
			)
		`, optionalHookUUID(handler.ID), policyID, handler.Decision, handler.Requirement, modifications, actions); err != nil {
			return fmt.Errorf("upsert managed hook handler %q: %w", definition.Key, err)
		}
	}
	r.managedWorkspaces[workspaceID] = struct{}{}
	return nil
}

func (r *PostgresHookRepository) Get(ctx context.Context, workspaceID, id string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	lookupID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	family, err := resolveHookFamily(ctx, r.db, wsID, lookupID, false)
	if err != nil {
		return HookPolicy{}, err
	}
	return r.familyResponse(ctx, r.db, family)
}

func (r *PostgresHookRepository) GetEffective(ctx context.Context, workspaceID, id string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	lookupID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	family, err := resolveHookFamily(ctx, r.db, wsID, lookupID, false)
	if err != nil {
		return HookPolicy{}, err
	}
	if !family.ActivePolicyID.Valid {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	var policy HookPolicy
	policy, _, err = scanHookPolicy(r.db.QueryRow(ctx, `
		SELECT `+hookPolicyColumns+`
		FROM cerebro_workflow_hook_policy
		WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
		family.ActivePolicyID, family.WorkspaceID, family.ID))
	if err != nil {
		return HookPolicy{}, err
	}
	if err := r.loadPolicyParts(ctx, &policy); err != nil {
		return HookPolicy{}, err
	}
	policy.FamilyID = util.UUIDToString(family.ID)
	policy.Lifecycle = HookLifecycle{
		State:        HookLifecycleLive,
		LivePolicyID: policy.ID,
		LiveVersion:  policy.Version,
	}
	if policy.Mode == HookModeManaged {
		policy.Lifecycle.State = HookLifecycleManaged
	} else if family.Disabled {
		policy.Lifecycle.State = HookLifecycleOff
	} else if family.CurrentDraftRevisionID.Valid {
		policy.Lifecycle.State = HookLifecycleLiveWithDraft
	}
	if family.CurrentDraftRevisionID.Valid {
		policy.Lifecycle.DraftID = util.UUIDToString(family.CurrentDraftRevisionID)
		policy.Lifecycle.LiveUnchangedByDraft = true
		var draftSeriesID pgtype.UUID
		if err := r.db.QueryRow(ctx, `
			SELECT draft_series_id, revision
			FROM cerebro_workflow_hook_draft_revision
			WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
			family.CurrentDraftRevisionID, family.WorkspaceID, family.ID,
		).Scan(&draftSeriesID, &policy.Lifecycle.DraftRevision); err != nil {
			return HookPolicy{}, err
		}
		policy.Lifecycle.DraftSeriesID = util.UUIDToString(draftSeriesID)
	}
	return policy, nil
}

func (r *PostgresHookRepository) Create(ctx context.Context, workspaceID string, actor HookPermissionActor, policy HookPolicy) (HookPolicy, error) {
	return r.saveDraft(ctx, workspaceID, actor, "", policy)
}

func (r *PostgresHookRepository) Update(ctx context.Context, workspaceID string, actor HookPermissionActor, id string, policy HookPolicy) (HookPolicy, error) {
	return r.saveDraft(ctx, workspaceID, actor, id, policy)
}

type hookFamilyRecord struct {
	ID                     pgtype.UUID
	WorkspaceID            pgtype.UUID
	ActivePolicyID         pgtype.UUID
	CurrentDraftRevisionID pgtype.UUID
	Disabled               bool
}

type hookTxBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

func (r *PostgresHookRepository) withTx(ctx context.Context, run func(pgx.Tx) (HookPolicy, error)) (HookPolicy, error) {
	beginner, ok := r.db.(hookTxBeginner)
	if !ok {
		return HookPolicy{}, errors.New("workflow hook lifecycle requires transaction support")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return HookPolicy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	policy, err := run(tx)
	if err != nil {
		return HookPolicy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return HookPolicy{}, err
	}
	return policy, nil
}

func resolveHookFamily(ctx context.Context, db cerebrodb.DBTX, workspaceID, lookupID pgtype.UUID, lock bool) (hookFamilyRecord, error) {
	query := `
		SELECT family.id, family.workspace_id, family.active_policy_id,
		       family.current_draft_revision_id, family.disabled
		FROM cerebro_workflow_hook_family family
		WHERE family.workspace_id=$1
		  AND (
		    family.id=$2
		    OR family.active_policy_id=$2
		    OR family.current_draft_revision_id=$2
		    OR EXISTS (
		      SELECT 1 FROM cerebro_workflow_hook_policy policy
		      WHERE policy.workspace_id=family.workspace_id
		        AND policy.family_id=family.id
		        AND policy.id=$2
		    )
		    OR EXISTS (
		      SELECT 1 FROM cerebro_workflow_hook_draft_revision revision
		      WHERE revision.workspace_id=family.workspace_id
		        AND revision.family_id=family.id
		        AND revision.id=$2
		    )
		  )`
	if lock {
		query += ` FOR UPDATE`
	}
	var family hookFamilyRecord
	err := db.QueryRow(ctx, query, workspaceID, lookupID).Scan(
		&family.ID,
		&family.WorkspaceID,
		&family.ActivePolicyID,
		&family.CurrentDraftRevisionID,
		&family.Disabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return hookFamilyRecord{}, ErrHookPolicyNotFound
	}
	return family, err
}

func (r *PostgresHookRepository) saveDraft(ctx context.Context, workspaceID string, actor HookPermissionActor, id string, policy HookPolicy) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	actorID, err := util.ParseUUID(actor.ID)
	if err != nil {
		return HookPolicy{}, err
	}
	var lookupID pgtype.UUID
	if id != "" {
		lookupID, err = util.ParseUUID(id)
		if err != nil {
			return HookPolicy{}, err
		}
	}
	return r.withTx(ctx, func(tx pgx.Tx) (HookPolicy, error) {
		var family hookFamilyRecord
		if id == "" {
			family.ID, _ = util.ParseUUID(uuid.NewString())
			family.WorkspaceID = wsID
			if _, err := tx.Exec(ctx, `
				INSERT INTO cerebro_workflow_hook_family (id, workspace_id)
				VALUES ($1,$2)`, family.ID, wsID); err != nil {
				return HookPolicy{}, err
			}
		} else {
			var err error
			family, err = resolveHookFamily(ctx, tx, wsID, lookupID, true)
			if err != nil {
				return HookPolicy{}, err
			}
			if family.ActivePolicyID.Valid {
				var mode HookMode
				if err := tx.QueryRow(ctx, `
					SELECT mode FROM cerebro_workflow_hook_policy
					WHERE id=$1 AND workspace_id=$2`, family.ActivePolicyID, wsID).Scan(&mode); err != nil {
					return HookPolicy{}, err
				}
				if mode == HookModeManaged && !actor.IsOwner {
					return HookPolicy{}, ErrManagedHookLocked
				}
			}
		}

		var seriesID pgtype.UUID
		revision := 1
		candidateVersion := 1
		if family.CurrentDraftRevisionID.Valid {
			var currentRevision int
			if err := tx.QueryRow(ctx, `
				SELECT draft_series_id, revision + 1, candidate_version
				FROM cerebro_workflow_hook_draft_revision
				WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
				family.CurrentDraftRevisionID, wsID, family.ID,
			).Scan(&seriesID, &revision, &candidateVersion); err != nil {
				return HookPolicy{}, err
			}
			currentRevision = revision - 1
			if policy.Revision > 0 && policy.Revision != currentRevision {
				return HookPolicy{}, ErrHookDraftRevisionStale
			}
		} else {
			seriesID, _ = util.ParseUUID(uuid.NewString())
			if err := tx.QueryRow(ctx, `
				SELECT COALESCE(MAX(policy_version),0)+1
				FROM cerebro_workflow_hook_policy
				WHERE workspace_id=$1 AND family_id=$2`, wsID, family.ID).Scan(&candidateVersion); err != nil {
				return HookPolicy{}, err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO cerebro_workflow_hook_draft_series
					(id, family_id, workspace_id)
				VALUES ($1,$2,$3)`, seriesID, family.ID, wsID); err != nil {
				return HookPolicy{}, err
			}
		}

		policy.ID = ""
		policy.FamilyID = util.UUIDToString(family.ID)
		policy.DraftSeriesID = util.UUIDToString(seriesID)
		policy.Revision = revision
		policy.Version = candidateVersion
		policy.Mode = HookModeDryRun
		policy.CreatedByID = actor.ID
		policy.CreatedByType = actor.Type
		policy.Lifecycle = HookLifecycle{}
		policy.LastRunAt = nil
		policy.ObservedRuns = 0
		policy.BaselineAt = nil
		policy.CanPublish = false
		if policy.FailMode == HookFailMode("open") {
			policy.FailMode = HookFailWarn
		}
		if policy.ConditionMode == "" {
			policy.ConditionMode = HookConditionAll
		}
		configuration, err := json.Marshal(policy)
		if err != nil {
			return HookPolicy{}, err
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(configuration))
		var revisionID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO cerebro_workflow_hook_draft_revision (
				draft_series_id, family_id, workspace_id, candidate_version,
				revision, configuration, configuration_hash, created_by_id, created_by_type
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING id`,
			seriesID, family.ID, wsID, candidateVersion, revision, configuration, hash, actorID, actor.Type,
		).Scan(&revisionID); err != nil {
			return HookPolicy{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_family
			SET current_draft_revision_id=$3, updated_at=now()
			WHERE workspace_id=$1 AND id=$2`, wsID, family.ID, revisionID); err != nil {
			return HookPolicy{}, err
		}
		family.CurrentDraftRevisionID = revisionID
		return r.familyResponse(ctx, tx, family)
	})
}

func (r *PostgresHookRepository) familyResponse(ctx context.Context, db cerebrodb.DBTX, family hookFamilyRecord) (HookPolicy, error) {
	var policy HookPolicy
	if family.CurrentDraftRevisionID.Valid {
		var configuration []byte
		var createdByID pgtype.UUID
		var createdByType string
		if err := db.QueryRow(ctx, `
			SELECT id, draft_series_id, candidate_version, revision, configuration,
			       created_by_id, created_by_type, created_at
			FROM cerebro_workflow_hook_draft_revision
			WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
			family.CurrentDraftRevisionID, family.WorkspaceID, family.ID,
		).Scan(&family.CurrentDraftRevisionID, &policy.DraftSeriesID, &policy.Version, &policy.Revision,
			&configuration, &createdByID, &createdByType, &policy.UpdatedAt); err != nil {
			return HookPolicy{}, err
		}
		if err := json.Unmarshal(configuration, &policy); err != nil {
			return HookPolicy{}, err
		}
		policy.ID = util.UUIDToString(family.CurrentDraftRevisionID)
		policy.FamilyID = util.UUIDToString(family.ID)
		policy.Mode = HookModeDryRun
		policy.CreatedByID = util.UUIDToString(createdByID)
		policy.CreatedByType = createdByType
		var baseline, lastRun pgtype.Timestamptz
		if err := db.QueryRow(ctx, `
			SELECT COUNT(evidence.id),MAX(evidence.qualified_at),MAX(run.created_at)
			FROM cerebro_workflow_hook_test_evidence evidence
			LEFT JOIN cerebro_workflow_hook_run run ON run.id=evidence.hook_run_id
			WHERE evidence.workspace_id=$1 AND evidence.draft_revision_id=$2`,
			family.WorkspaceID, family.CurrentDraftRevisionID,
		).Scan(&policy.ObservedRuns, &baseline, &lastRun); err != nil {
			return HookPolicy{}, err
		}
		if baseline.Valid {
			value := baseline.Time
			policy.BaselineAt = &value
		}
		if lastRun.Valid {
			value := lastRun.Time
			policy.LastRunAt = &value
		}
		policy.CanPublish = policy.ObservedRuns > 0 && baseline.Valid
	} else if family.ActivePolicyID.Valid {
		var err error
		policy, _, err = scanHookPolicy(db.QueryRow(ctx, `
			SELECT `+hookPolicyColumns+`
			FROM cerebro_workflow_hook_policy
			WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
			family.ActivePolicyID, family.WorkspaceID, family.ID))
		if err != nil {
			return HookPolicy{}, err
		}
		if err := r.loadPolicyPartsWith(ctx, db, &policy); err != nil {
			return HookPolicy{}, err
		}
		if err := r.loadPolicyMetricsWith(ctx, db, &policy); err != nil {
			return HookPolicy{}, err
		}
	} else {
		return HookPolicy{}, ErrHookPolicyNotFound
	}
	policy.FamilyID = util.UUIDToString(family.ID)
	policy.Lifecycle = HookLifecycle{
		State:                HookLifecycleDraft,
		LiveUnchangedByDraft: family.ActivePolicyID.Valid && family.CurrentDraftRevisionID.Valid,
	}
	if family.ActivePolicyID.Valid {
		policy.Lifecycle.LivePolicyID = util.UUIDToString(family.ActivePolicyID)
		var activeMode HookMode
		if err := db.QueryRow(ctx, `
			SELECT policy_version, mode FROM cerebro_workflow_hook_policy
			WHERE id=$1 AND workspace_id=$2`, family.ActivePolicyID, family.WorkspaceID,
		).Scan(&policy.Lifecycle.LiveVersion, &activeMode); err != nil {
			return HookPolicy{}, err
		}
		switch {
		case activeMode == HookModeManaged:
			policy.Lifecycle.State = HookLifecycleManaged
		case family.Disabled && family.CurrentDraftRevisionID.Valid:
			policy.Lifecycle.State = HookLifecycleOffWithDraft
		case family.Disabled:
			policy.Lifecycle.State = HookLifecycleOff
		case family.CurrentDraftRevisionID.Valid:
			policy.Lifecycle.State = HookLifecycleLiveWithDraft
		default:
			policy.Lifecycle.State = HookLifecycleLive
		}
	}
	if family.CurrentDraftRevisionID.Valid {
		policy.Lifecycle.DraftID = util.UUIDToString(family.CurrentDraftRevisionID)
		policy.Lifecycle.DraftSeriesID = policy.DraftSeriesID
		policy.Lifecycle.DraftRevision = policy.Revision
	}
	return policy, nil
}

func (r *PostgresHookRepository) Disable(ctx context.Context, workspaceID string, actor HookPermissionActor, id string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	lookupID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	return r.withTx(ctx, func(tx pgx.Tx) (HookPolicy, error) {
		family, err := resolveHookFamily(ctx, tx, wsID, lookupID, true)
		if err != nil {
			return HookPolicy{}, err
		}
		if !family.ActivePolicyID.Valid {
			return HookPolicy{}, ErrHookPolicyNotFound
		}
		var mode HookMode
		if err := tx.QueryRow(ctx, `
			SELECT mode FROM cerebro_workflow_hook_policy
			WHERE id=$1 AND workspace_id=$2`, family.ActivePolicyID, wsID).Scan(&mode); err != nil {
			return HookPolicy{}, err
		}
		if mode == HookModeManaged && !actor.IsOwner {
			return HookPolicy{}, ErrManagedHookLocked
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_policy
			SET mode='off', updated_at=now()
			WHERE id=$1 AND workspace_id=$2`, family.ActivePolicyID, wsID); err != nil {
			return HookPolicy{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_family
			SET disabled=TRUE, updated_at=now()
			WHERE id=$1 AND workspace_id=$2`, family.ID, wsID); err != nil {
			return HookPolicy{}, err
		}
		family.Disabled = true
		return r.familyResponse(ctx, tx, family)
	})
}

func (r *PostgresHookRepository) DiscardDraft(ctx context.Context, workspaceID string, actor HookPermissionActor, id string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	lookupID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	return r.withTx(ctx, func(tx pgx.Tx) (HookPolicy, error) {
		family, err := resolveHookFamily(ctx, tx, wsID, lookupID, true)
		if err != nil {
			return HookPolicy{}, err
		}
		if !family.CurrentDraftRevisionID.Valid {
			return HookPolicy{}, ErrHookPolicyNotFound
		}
		if family.ActivePolicyID.Valid {
			var mode HookMode
			if err := tx.QueryRow(ctx, `
				SELECT mode FROM cerebro_workflow_hook_policy
				WHERE id=$1 AND workspace_id=$2`, family.ActivePolicyID, wsID).Scan(&mode); err != nil {
				return HookPolicy{}, err
			}
			if mode == HookModeManaged && !actor.IsOwner {
				return HookPolicy{}, ErrManagedHookLocked
			}
		}
		var seriesID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			SELECT draft_series_id
			FROM cerebro_workflow_hook_draft_revision
			WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
			family.CurrentDraftRevisionID, wsID, family.ID,
		).Scan(&seriesID); err != nil {
			return HookPolicy{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_draft_series
			SET status='discarded', discarded_at=now()
			WHERE id=$1 AND workspace_id=$2 AND family_id=$3 AND status='active'`,
			seriesID, wsID, family.ID,
		); err != nil {
			return HookPolicy{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_family
			SET current_draft_revision_id=NULL, updated_at=now()
			WHERE id=$1 AND workspace_id=$2`, family.ID, wsID); err != nil {
			return HookPolicy{}, err
		}
		family.CurrentDraftRevisionID = pgtype.UUID{}
		if !family.ActivePolicyID.Valid {
			return HookPolicy{
				FamilyID: util.UUIDToString(family.ID),
				Lifecycle: HookLifecycle{
					State: HookLifecycleOff,
				},
			}, nil
		}
		return r.familyResponse(ctx, tx, family)
	})
}

func (r *PostgresHookRepository) Delete(ctx context.Context, workspaceID string, actor HookPermissionActor, id string) error {
	current, err := r.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if current.Lifecycle.State == HookLifecycleManaged && !actor.IsOwner {
		return ErrManagedHookLocked
	}
	familyID, err := util.ParseUUID(current.FamilyID)
	if err != nil {
		return err
	}
	wsID, _ := util.ParseUUID(workspaceID)
	beginner, ok := r.db.(hookTxBeginner)
	if !ok {
		return errors.New("workflow hook lifecycle requires transaction support")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		DELETE FROM cerebro_workflow_hook_family
		WHERE workspace_id=$1 AND id=$2`, wsID, familyID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		DELETE FROM cerebro_workflow_hook_policy
		WHERE workspace_id=$1 AND family_id=$2`, wsID, familyID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresHookRepository) insertVersion(ctx context.Context, workspaceID string, actor HookPermissionActor, familyID string, version int, policy HookPolicy) (HookPolicy, error) {
	if policy.FailMode == HookFailMode("open") {
		policy.FailMode = HookFailWarn
	}
	if policy.ConditionMode == "" {
		policy.ConditionMode = HookConditionAll
	}
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	familyUUID, err := util.ParseUUID(familyID)
	if err != nil {
		return HookPolicy{}, err
	}
	actorID, err := util.ParseUUID(actor.ID)
	if err != nil {
		return HookPolicy{}, err
	}
	eventsJSON, _ := json.Marshal(policy.Events)
	conditionsJSON, _ := json.Marshal(policy.Conditions)
	insert := `INSERT INTO cerebro_workflow_hook_policy
(family_id, workspace_id, name, description, policy_version, mode, fail_mode, condition_mode, event_types, conditions, created_by_id, created_by_type)
VALUES ($1,$2,$3,$4,$5,'dry_run',$6,$7,$8,$9,$10,$11) RETURNING ` + hookPolicyColumns
	created, _, err := scanHookPolicy(r.db.QueryRow(ctx, insert, familyUUID, wsID, policy.Name, policy.Description, version, policy.FailMode, policy.ConditionMode, eventsJSON, conditionsJSON, actorID, actor.Type))
	if err != nil {
		return HookPolicy{}, err
	}
	if err := r.insertPolicyParts(ctx, created.ID, policy.Bindings, policy.Handlers); err != nil {
		return HookPolicy{}, err
	}
	created.Bindings = policy.Bindings
	created.Handlers = policy.Handlers
	return created, nil
}

func (r *PostgresHookRepository) insertPolicyParts(ctx context.Context, policyID string, bindings []HookBinding, handlers []HookHandler) error {
	return r.insertPolicyPartsWith(ctx, r.db, policyID, bindings, handlers)
}

func (r *PostgresHookRepository) insertPolicyPartsWith(ctx context.Context, db cerebrodb.DBTX, policyID string, bindings []HookBinding, handlers []HookHandler) error {
	id, err := util.ParseUUID(policyID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if _, err := db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_binding (policy_id, scope_kind, scope_id, priority) VALUES ($1,$2,$3,$4)`, id, binding.Kind, binding.ID, binding.Priority); err != nil {
			return err
		}
	}
	for position, handler := range handlers {
		modifications, _ := json.Marshal(handler.Modifications)
		actions, _ := json.Marshal(handler.Actions)
		if _, err := db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_handler (policy_id, position, decision, requirement, modifications, actions) VALUES ($1,$2,$3,$4,$5,$6)`, id, position, handler.Decision, handler.Requirement, modifications, actions); err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresHookRepository) loadPolicyParts(ctx context.Context, policy *HookPolicy) error {
	return r.loadPolicyPartsWith(ctx, r.db, policy)
}

func (r *PostgresHookRepository) loadPolicyPartsWith(ctx context.Context, db cerebrodb.DBTX, policy *HookPolicy) error {
	id, err := util.ParseUUID(policy.ID)
	if err != nil {
		return err
	}
	bindingRows, err := db.Query(ctx, `SELECT scope_kind, scope_id, priority FROM cerebro_workflow_hook_binding WHERE policy_id=$1 ORDER BY priority DESC, created_at`, id)
	if err != nil {
		return err
	}
	for bindingRows.Next() {
		var binding HookBinding
		if err := bindingRows.Scan(&binding.Kind, &binding.ID, &binding.Priority); err != nil {
			bindingRows.Close()
			return err
		}
		policy.Bindings = append(policy.Bindings, binding)
	}
	if err := bindingRows.Err(); err != nil {
		bindingRows.Close()
		return err
	}
	bindingRows.Close()

	handlerRows, err := db.Query(ctx, `SELECT id, decision, requirement, modifications, actions FROM cerebro_workflow_hook_handler WHERE policy_id=$1 ORDER BY position`, id)
	if err != nil {
		return err
	}
	for handlerRows.Next() {
		var handlerID pgtype.UUID
		var handler HookHandler
		var modifications, actions []byte
		if err := handlerRows.Scan(&handlerID, &handler.Decision, &handler.Requirement, &modifications, &actions); err != nil {
			handlerRows.Close()
			return err
		}
		handler.ID = util.UUIDToString(handlerID)
		_ = json.Unmarshal(modifications, &handler.Modifications)
		_ = json.Unmarshal(actions, &handler.Actions)
		policy.Handlers = append(policy.Handlers, handler)
	}
	if err := handlerRows.Err(); err != nil {
		handlerRows.Close()
		return err
	}
	handlerRows.Close()
	return nil
}

func (r *PostgresHookRepository) loadPolicyMetrics(ctx context.Context, policy *HookPolicy) error {
	return r.loadPolicyMetricsWith(ctx, r.db, policy)
}

func (r *PostgresHookRepository) loadPolicyMetricsWith(ctx context.Context, db cerebrodb.DBTX, policy *HookPolicy) error {
	id, err := util.ParseUUID(policy.ID)
	if err != nil {
		return err
	}
	var count int
	var lastRun pgtype.Timestamptz
	if err := db.QueryRow(ctx, `SELECT COUNT(*), MAX(created_at) FROM cerebro_workflow_hook_run WHERE policy_id=$1`, id).Scan(&count, &lastRun); err != nil {
		return err
	}
	policy.ObservedRuns = count
	if lastRun.Valid {
		value := lastRun.Time
		policy.LastRunAt = &value
	}
	policy.CanPublish = policy.Mode == HookModeDryRun && count > 0 && policy.BaselineAt != nil
	return nil
}

func (r *PostgresHookRepository) Publish(ctx context.Context, workspaceID, id, actorID string) (HookPolicy, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookPolicy{}, err
	}
	lookupID, err := util.ParseUUID(id)
	if err != nil {
		return HookPolicy{}, err
	}
	publisherID, err := util.ParseUUID(actorID)
	if err != nil {
		return HookPolicy{}, err
	}
	return r.withTx(ctx, func(tx pgx.Tx) (HookPolicy, error) {
		family, err := resolveHookFamily(ctx, tx, wsID, lookupID, true)
		if err != nil {
			return HookPolicy{}, err
		}
		if !family.CurrentDraftRevisionID.Valid {
			return HookPolicy{}, ErrHookPublishPrerequisite
		}
		draft, err := r.familyResponse(ctx, tx, family)
		if err != nil {
			return HookPolicy{}, err
		}
		if !draft.CanPublish {
			return HookPolicy{}, ErrHookPublishPrerequisite
		}
		canonicalizeWorkspaceBindings(&draft, workspaceID)
		eventsJSON, _ := json.Marshal(draft.Events)
		conditionsJSON, _ := json.Marshal(draft.Conditions)
		var published HookPolicy
		published, _, err = scanHookPolicy(tx.QueryRow(ctx, `
			UPDATE cerebro_workflow_hook_policy
			SET name=$4, description=$5, mode='enforce', fail_mode=$6,
			    condition_mode=$7, event_types=$8, conditions=$9,
			    published_by_id=$10, published_at=now(), updated_at=now()
			WHERE family_id=$1 AND workspace_id=$2 AND policy_version=$3
			  AND mode='dry_run'
			RETURNING `+hookPolicyColumns,
			family.ID, wsID, draft.Version, draft.Name, draft.Description,
			draft.FailMode, draft.ConditionMode, eventsJSON, conditionsJSON,
			publisherID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			published, _, err = scanHookPolicy(tx.QueryRow(ctx, `
				INSERT INTO cerebro_workflow_hook_policy
					(family_id,workspace_id,name,description,policy_version,mode,fail_mode,
					 condition_mode,event_types,conditions,baseline_at,published_by_id,published_at,
					 created_by_id,created_by_type)
				VALUES ($1,$2,$3,$4,$5,'enforce',$6,$7,$8,$9,now(),$10,now(),$10,'member')
				RETURNING `+hookPolicyColumns,
				family.ID, wsID, draft.Name, draft.Description, draft.Version,
				draft.FailMode, draft.ConditionMode, eventsJSON, conditionsJSON, publisherID,
			))
		}
		if err != nil {
			return HookPolicy{}, err
		}
		publishedID, _ := util.ParseUUID(published.ID)
		if _, err := tx.Exec(ctx, `DELETE FROM cerebro_workflow_hook_handler WHERE policy_id=$1`, publishedID); err != nil {
			return HookPolicy{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM cerebro_workflow_hook_binding WHERE policy_id=$1`, publishedID); err != nil {
			return HookPolicy{}, err
		}
		if err := r.insertPolicyPartsWith(ctx, tx, published.ID, draft.Bindings, draft.Handlers); err != nil {
			return HookPolicy{}, err
		}
		var seriesID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			UPDATE cerebro_workflow_hook_draft_revision
			SET published_policy_id=$2, updated_at=now()
			WHERE id=$1 AND workspace_id=$3 AND family_id=$4
			RETURNING draft_series_id`,
			family.CurrentDraftRevisionID, optionalHookUUID(published.ID), wsID, family.ID,
		).Scan(&seriesID); err != nil {
			return HookPolicy{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_draft_series
			SET status='published', published_at=now()
			WHERE id=$1 AND workspace_id=$2 AND family_id=$3`,
			seriesID, wsID, family.ID,
		); err != nil {
			return HookPolicy{}, err
		}
		if family.ActivePolicyID.Valid {
			if _, err := tx.Exec(ctx, `
				UPDATE cerebro_workflow_hook_policy
				SET mode='off', updated_at=now()
				WHERE id=$1 AND workspace_id=$2`, family.ActivePolicyID, wsID); err != nil {
				return HookPolicy{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cerebro_workflow_hook_family
			SET active_policy_id=$3, current_draft_revision_id=NULL,
			    disabled=FALSE, updated_at=now()
			WHERE workspace_id=$1 AND id=$2`, wsID, family.ID, publishedID); err != nil {
			return HookPolicy{}, err
		}
		family.ActivePolicyID = publishedID
		family.CurrentDraftRevisionID = pgtype.UUID{}
		family.Disabled = false
		return r.familyResponse(ctx, tx, family)
	})
}

func (r *PostgresHookRepository) Runs(ctx context.Context, workspaceID, policyID string) ([]HookRunRecord, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	lookupID, err := util.ParseUUID(policyID)
	if err != nil {
		return nil, err
	}
	family, err := resolveHookFamily(ctx, r.db, wsID, lookupID, false)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT run.id, COALESCE(run.policy_id,run.draft_revision_id), run.policy_version, run.input_event, run.source_scope,
		       run.decision, run.would_decision, run.matched_conditions, run.fail_mode,
		       run.remediation, run.latency_ms, run.timed_out, run.created_at
		FROM cerebro_workflow_hook_run run
		LEFT JOIN cerebro_workflow_hook_policy policy ON policy.id=run.policy_id
		LEFT JOIN cerebro_workflow_hook_draft_revision revision ON revision.id=run.draft_revision_id
		WHERE run.workspace_id=$1 AND COALESCE(policy.family_id,revision.family_id)=$2
		ORDER BY run.created_at DESC
		LIMIT 200`, wsID, family.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HookRunRecord
	for rows.Next() {
		var runID, runPolicyID pgtype.UUID
		var eventJSON, scopeJSON, matchesJSON []byte
		var decision string
		var wouldDecision pgtype.Text
		var remediation string
		var timedOut bool
		var created pgtype.Timestamptz
		var run HookRunRecord
		if err := rows.Scan(&runID, &runPolicyID, &run.PolicyVersion, &eventJSON, &scopeJSON, &decision, &wouldDecision, &matchesJSON, &run.FailMode, &remediation, &run.LatencyMS, &timedOut, &created); err != nil {
			return nil, err
		}
		run.ID = util.UUIDToString(runID)
		run.PolicyID = util.UUIDToString(runPolicyID)
		run.CreatedAt = created.Time
		_ = json.Unmarshal(eventJSON, &run.Event)
		_ = json.Unmarshal(scopeJSON, &run.SourceScope)
		run.Result = HookResult{Decision: HookDecision(decision), WouldDecision: HookDecision(wouldDecision.String), TimedOut: timedOut}
		_ = json.Unmarshal(matchesJSON, &run.Result.MatchedConditions)
		if remediation != "" {
			run.Result.Requirements = strings.Split(remediation, "\n")
		}
		actionRows, actionErr := r.db.Query(ctx, `SELECT action_index,action_type,status,result,error FROM cerebro_workflow_hook_action_run WHERE hook_run_id=$1 ORDER BY action_index`, runID)
		if actionErr != nil {
			return nil, actionErr
		}
		for actionRows.Next() {
			var action HookActionResult
			var resultJSON []byte
			if err := actionRows.Scan(&action.ActionIndex, &action.Type, &action.Status, &resultJSON, &action.Error); err != nil {
				actionRows.Close()
				return nil, err
			}
			_ = json.Unmarshal(resultJSON, &action.Result)
			run.Result.ActionResults = append(run.Result.ActionResults, action)
		}
		if err := actionRows.Err(); err != nil {
			actionRows.Close()
			return nil, err
		}
		actionRows.Close()
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *PostgresHookRepository) RecordRun(ctx context.Context, workspaceID string, run HookRunRecord) error {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return err
	}
	policyID, err := util.ParseUUID(run.PolicyID)
	if err != nil {
		return err
	}
	policy, err := r.Get(ctx, workspaceID, run.PolicyID)
	if err != nil {
		return err
	}
	eventJSON, _ := json.Marshal(run.Event)
	scopeJSON, _ := json.Marshal(run.SourceScope)
	conditionsJSON, _ := json.Marshal(run.Result.MatchedConditions)
	would := pgtype.Text{String: string(run.Result.WouldDecision), Valid: run.Result.WouldDecision != ""}
	key := fmt.Sprintf("%s:%d:test", run.Event.EventID, run.PolicyVersion)
	var hookRunID pgtype.UUID
	err = r.db.QueryRow(ctx, `INSERT INTO cerebro_workflow_hook_run
(workspace_id,policy_id,policy_version,event_id,event_type,source_scope,input_event,matched_conditions,decision,would_decision,fail_mode,remediation,latency_ms,timed_out,idempotency_key)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (workspace_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id`, wsID, policyID, run.PolicyVersion, run.Event.EventID, run.Event.Type, scopeJSON, eventJSON, conditionsJSON, run.Result.Decision, would, policy.FailMode, strings.Join(run.Result.Requirements, "\n"), run.LatencyMS, run.Result.TimedOut, key).Scan(&hookRunID)
	if err != nil {
		return err
	}
	for _, action := range run.Result.ActionResults {
		configJSON, _ := json.Marshal(action.Config)
		resultJSON, _ := json.Marshal(action.Result)
		_, err = r.db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_action_run
(hook_run_id,handler_id,action_index,action_type,action_config,status,result,error,started_at,finished_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now(),now()) ON CONFLICT (hook_run_id,action_index) DO NOTHING`,
			hookRunID, optionalHookUUID(action.HandlerID), action.ActionIndex, action.Type, configJSON, action.Status, resultJSON, action.Error)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresHookRepository) RefreshBaseline(ctx context.Context, workspaceID, policyID string) (time.Time, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return time.Time{}, err
	}
	id, err := util.ParseUUID(policyID)
	if err != nil {
		return time.Time{}, err
	}
	var baseline pgtype.Timestamptz
	err = r.db.QueryRow(ctx, `UPDATE cerebro_workflow_hook_policy p SET baseline_at=now(), updated_at=now()
WHERE p.id=$1 AND p.workspace_id=$2 AND p.mode='dry_run'
AND EXISTS (SELECT 1 FROM cerebro_workflow_hook_run r WHERE r.policy_id=p.id)
RETURNING p.baseline_at`, id, wsID).Scan(&baseline)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrHookPublishPrerequisite
	}
	if err != nil {
		return time.Time{}, err
	}
	return baseline.Time, nil
}

func (r *PostgresHookRepository) RecordTestEvidence(ctx context.Context, workspaceID, eventJournalID string, run HookRunRecord) (time.Time, error) {
	if run.Result.TimedOut || len(run.Result.Matches) == 0 {
		return time.Time{}, ErrHookPublishPrerequisite
	}
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return time.Time{}, err
	}
	revisionID, err := util.ParseUUID(run.PolicyID)
	if err != nil {
		return time.Time{}, err
	}
	journalID, err := util.ParseUUID(eventJournalID)
	if err != nil {
		return time.Time{}, ErrHookEventNotFound
	}
	var familyID pgtype.UUID
	var candidateVersion int
	err = r.db.QueryRow(ctx, `
		SELECT revision.family_id,revision.candidate_version
		FROM cerebro_workflow_hook_draft_revision revision
		JOIN cerebro_workflow_hook_family family
		  ON family.workspace_id=revision.workspace_id
		 AND family.id=revision.family_id
		 AND family.current_draft_revision_id=revision.id
		WHERE revision.workspace_id=$1 AND revision.id=$2`,
		wsID, revisionID,
	).Scan(&familyID, &candidateVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrHookDraftRevisionStale
	}
	if err != nil {
		return time.Time{}, err
	}
	if candidateVersion != run.PolicyVersion {
		return time.Time{}, ErrHookDraftRevisionStale
	}
	var retained bool
	if err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM cerebro_workflow_hook_event_journal
			WHERE workspace_id=$1 AND id=$2 AND expires_at > now()
		)`, wsID, journalID).Scan(&retained); err != nil {
		return time.Time{}, err
	}
	if !retained {
		return time.Time{}, ErrHookEventNotFound
	}
	eventJSON, _ := json.Marshal(run.Event)
	scopeJSON, _ := json.Marshal(run.SourceScope)
	conditionsJSON, _ := json.Marshal(run.Result.MatchedConditions)
	would := pgtype.Text{String: string(run.Result.WouldDecision), Valid: run.Result.WouldDecision != ""}
	key := fmt.Sprintf("%s:%s:test", eventJournalID, run.PolicyID)
	var hookRunID pgtype.UUID
	err = r.db.QueryRow(ctx, `INSERT INTO cerebro_workflow_hook_run
		(workspace_id,policy_id,draft_revision_id,policy_version,event_id,event_type,
		 source_scope,input_event,matched_conditions,decision,would_decision,fail_mode,
		 remediation,latency_ms,timed_out,idempotency_key)
		VALUES ($1,NULL,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (workspace_id,idempotency_key) DO UPDATE
			SET idempotency_key=EXCLUDED.idempotency_key
		RETURNING id`,
		wsID, revisionID, run.PolicyVersion, run.Event.EventID, run.Event.Type,
		scopeJSON, eventJSON, conditionsJSON, run.Result.Decision, would, run.FailMode,
		strings.Join(run.Result.Requirements, "\n"), run.LatencyMS, run.Result.TimedOut, key,
	).Scan(&hookRunID)
	if err != nil {
		return time.Time{}, err
	}
	for _, action := range run.Result.ActionResults {
		configJSON, _ := json.Marshal(action.Config)
		resultJSON, _ := json.Marshal(action.Result)
		if _, err := r.db.Exec(ctx, `INSERT INTO cerebro_workflow_hook_action_run
			(hook_run_id,handler_id,action_index,action_type,action_config,status,result,error,started_at,finished_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now(),now())
			ON CONFLICT (hook_run_id,action_index) DO NOTHING`,
			hookRunID, optionalHookUUID(action.HandlerID), action.ActionIndex, action.Type,
			configJSON, action.Status, resultJSON, action.Error,
		); err != nil {
			return time.Time{}, err
		}
	}
	var qualifiedAt pgtype.Timestamptz
	err = r.db.QueryRow(ctx, `INSERT INTO cerebro_workflow_hook_test_evidence
		(workspace_id,family_id,draft_revision_id,event_journal_id,hook_run_id)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (workspace_id,draft_revision_id) DO UPDATE
			SET event_journal_id=EXCLUDED.event_journal_id,
			    hook_run_id=EXCLUDED.hook_run_id,
			    qualified_at=now()
		RETURNING qualified_at`,
		wsID, familyID, revisionID, journalID, hookRunID,
	).Scan(&qualifiedAt)
	if err != nil {
		return time.Time{}, err
	}
	return qualifiedAt.Time, nil
}

func (r *PostgresHookRepository) CaptureEvent(ctx context.Context, workspaceID string, event HookEvent) (HookJournalEvent, error) {
	if _, ok := hookJournalSectionsByEvent[event.Type]; !ok {
		return HookJournalEvent{}, ErrHookEventNotRetainable
	}
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookJournalEvent{}, err
	}
	sanitized := sanitizeHookJournalEvent(workspaceID, event)
	replayJSON, err := json.Marshal(sanitized)
	if err != nil {
		return HookJournalEvent{}, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(replayJSON))
	if _, err := r.db.Exec(ctx, `
		DELETE FROM cerebro_workflow_hook_event_journal
		WHERE workspace_id=$1 AND expires_at <= now()`, wsID); err != nil {
		return HookJournalEvent{}, err
	}
	var id pgtype.UUID
	var occurredAt, expiresAt pgtype.Timestamptz
	err = r.db.QueryRow(ctx, `
		INSERT INTO cerebro_workflow_hook_event_journal
			(workspace_id,event_id,event_type,event_hash,replay_event)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (workspace_id,event_hash) DO UPDATE
			SET event_id=EXCLUDED.event_id
		RETURNING id,occurred_at,expires_at`,
		wsID, sanitized.EventID, sanitized.Type, hash, replayJSON,
	).Scan(&id, &occurredAt, &expiresAt)
	if err != nil {
		return HookJournalEvent{}, err
	}
	return HookJournalEvent{
		ID: util.UUIDToString(id), EventID: sanitized.EventID, EventType: sanitized.Type,
		SchemaVersion: 1, EventHash: hash, OccurredAt: occurredAt.Time, ExpiresAt: expiresAt.Time,
		replayEvent: sanitized,
	}, nil
}

func (r *PostgresHookRepository) CompatibleEvents(ctx context.Context, workspaceID, policyID string, limit int) ([]HookJournalEvent, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	policy, err := r.Get(ctx, workspaceID, policyID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	eventTypes := make([]string, 0, len(policy.Events))
	for _, eventType := range policy.Events {
		eventTypes = append(eventTypes, string(eventType))
	}
	if _, err := r.db.Exec(ctx, `
		DELETE FROM cerebro_workflow_hook_event_journal
		WHERE workspace_id=$1 AND expires_at <= now()`, wsID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `
		SELECT id,event_id,event_type,schema_version,event_hash,occurred_at,expires_at,replay_event
		FROM cerebro_workflow_hook_event_journal
		WHERE workspace_id=$1 AND event_type=ANY($2) AND expires_at > now()
		ORDER BY occurred_at DESC,id DESC
		LIMIT $3`, wsID, eventTypes, limit*5)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HookJournalEvent, 0, limit)
	for rows.Next() {
		var id pgtype.UUID
		var occurredAt, expiresAt pgtype.Timestamptz
		var replayJSON []byte
		var retained HookJournalEvent
		if err := rows.Scan(&id, &retained.EventID, &retained.EventType, &retained.SchemaVersion, &retained.EventHash, &occurredAt, &expiresAt, &replayJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(replayJSON, &retained.replayEvent); err != nil {
			continue
		}
		if !policyMatches(policy, retained.replayEvent) {
			continue
		}
		retained.ID = util.UUIDToString(id)
		retained.OccurredAt = occurredAt.Time
		retained.ExpiresAt = expiresAt.Time
		out = append(out, retained)
		if len(out) == limit {
			break
		}
	}
	return out, rows.Err()
}

func (r *PostgresHookRepository) ReplayEvent(ctx context.Context, workspaceID, eventID string) (HookEvent, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return HookEvent{}, err
	}
	id, err := util.ParseUUID(eventID)
	if err != nil {
		return HookEvent{}, ErrHookEventNotFound
	}
	var replayJSON []byte
	err = r.db.QueryRow(ctx, `
		SELECT replay_event
		FROM cerebro_workflow_hook_event_journal
		WHERE workspace_id=$1 AND id=$2 AND expires_at > now()`,
		wsID, id,
	).Scan(&replayJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return HookEvent{}, ErrHookEventNotFound
	}
	if err != nil {
		return HookEvent{}, err
	}
	var event HookEvent
	if err := json.Unmarshal(replayJSON, &event); err != nil {
		return HookEvent{}, err
	}
	return event, nil
}

type hookRowScanner interface {
	Scan(...any) error
}

func scanHookPolicy(row hookRowScanner) (HookPolicy, pgtype.UUID, error) {
	var id, familyID, workspaceID, createdByID, publishedByID pgtype.UUID
	var version int32
	var mode, failMode, conditionMode, name, description, createdByType string
	var eventJSON, conditionsJSON []byte
	var baselineAt, publishedAt, createdAt, updatedAt pgtype.Timestamptz
	if err := row.Scan(&id, &familyID, &workspaceID, &name, &description, &version, &mode, &failMode, &conditionMode, &eventJSON, &conditionsJSON, &baselineAt, &publishedAt, &createdByID, &createdByType, &publishedByID, &createdAt, &updatedAt); err != nil {
		return HookPolicy{}, pgtype.UUID{}, err
	}
	policy := HookPolicy{ID: util.UUIDToString(id), Version: int(version), Name: name, Description: description, Mode: HookMode(mode), FailMode: HookFailMode(failMode), ConditionMode: HookConditionMode(conditionMode), CreatedByID: util.UUIDToString(createdByID), CreatedByType: createdByType, UpdatedAt: updatedAt.Time}
	if baselineAt.Valid {
		baseline := baselineAt.Time
		policy.BaselineAt = &baseline
	}
	if err := json.Unmarshal(eventJSON, &policy.Events); err != nil {
		return HookPolicy{}, pgtype.UUID{}, err
	}
	if err := json.Unmarshal(conditionsJSON, &policy.Conditions); err != nil {
		return HookPolicy{}, pgtype.UUID{}, err
	}
	return policy, familyID, nil
}
