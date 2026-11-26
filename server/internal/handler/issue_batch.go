package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Label and property changes carried by a batch update (MUL-8882). They are
// operations, not replacement values: add and remove leave every other label
// on the issue alone, and clear + add is how a caller replaces the set.
type BatchLabelOps struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
	Clear  bool     `json:"clear"`
}

type BatchPropertyOps struct {
	Set   map[string]json.RawMessage `json:"set"`
	Unset []string                   `json:"unset"`
}

// maxBatchFieldOps bounds the label and property operations one request may
// carry; each one is a write per selected issue.
const maxBatchFieldOps = 50

// Per-issue outcomes of a batch update. `updated` in the response counts
// both written outcomes, so a client that predates `results` keeps reading a
// value-already-set issue as a success.
const (
	batchOutcomeUpdated     = "updated"
	batchOutcomeUnchanged   = "unchanged"
	batchOutcomeSkipped     = "skipped"
	batchOutcomeWouldUpdate = "would_update"
	batchOutcomeDeleted     = "deleted"
	batchOutcomeWouldDelete = "would_delete"
)

type batchIssueResult struct {
	IssueID    string `json:"issue_id"`
	Identifier string `json:"identifier,omitempty"`
	Outcome    string `json:"outcome"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	// RunOutcome is set on an updated item whose write could have started a
	// run; see IssueRunOutcome.
	RunOutcome *IssueRunOutcome `json:"run_outcome,omitempty"`
}

// batchSkipError ends one issue's write with a reason the caller can act on,
// rolling back everything written for that issue.
type batchSkipError struct {
	code    string
	message string
}

func (e *batchSkipError) Error() string { return e.message }

type batchLabelPlan struct {
	add    []pgtype.UUID
	remove []pgtype.UUID
	clear  bool
}

type batchPropertySet struct {
	id  pgtype.UUID
	raw json.RawMessage
}

type batchPropertyPlan struct {
	set   []batchPropertySet
	unset []pgtype.UUID
	// Advisory lock keys of the properties written, sorted so two batches
	// over overlapping properties take them in the same order.
	lockKeys []string
}

// resolveBatchLabelPlan validates the label operations once for the whole
// batch. A label that does not exist invalidates the request rather than
// skipping every issue one by one.
func (h *Handler) resolveBatchLabelPlan(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, ops *BatchLabelOps) (*batchLabelPlan, bool) {
	if ops == nil || (len(ops.Add) == 0 && len(ops.Remove) == 0 && !ops.Clear) {
		return nil, true
	}
	if len(ops.Add)+len(ops.Remove) > maxBatchFieldOps {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d label operations per batch update", maxBatchFieldOps))
		return nil, false
	}
	plan := &batchLabelPlan{clear: ops.Clear}
	seen := make(map[pgtype.UUID]bool, len(ops.Add)+len(ops.Remove))
	resolve := func(raw string, adding bool) (pgtype.UUID, bool, bool) {
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("label id %q must be a UUID", raw))
			return id, false, false
		}
		if wasAdd, dup := seen[id]; dup {
			if wasAdd != adding {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("label %s cannot be both added and removed", raw))
				return id, false, false
			}
			return id, false, true
		}
		label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("label %s not found in this workspace", raw))
				return id, false, false
			}
			slog.Warn("batch update issues: load label", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to validate labels")
			return id, false, false
		}
		if label.ResourceType != "issue" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("label %s is not an issue label", raw))
			return id, false, false
		}
		seen[id] = adding
		return id, true, true
	}
	for _, raw := range ops.Add {
		id, fresh, ok := resolve(raw, true)
		if !ok {
			return nil, false
		}
		if fresh {
			plan.add = append(plan.add, id)
		}
	}
	for _, raw := range ops.Remove {
		id, fresh, ok := resolve(raw, false)
		if !ok {
			return nil, false
		}
		if fresh {
			plan.remove = append(plan.remove, id)
		}
	}
	return plan, true
}

// resolveBatchPropertyPlan validates property definitions and values once for
// the whole batch, with the same rules as SetIssueProperty. The write re-checks
// each definition under its lock, since it can change between here and there.
func (h *Handler) resolveBatchPropertyPlan(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, ops *BatchPropertyOps) (*batchPropertyPlan, bool) {
	if ops == nil || (len(ops.Set) == 0 && len(ops.Unset) == 0) {
		return nil, true
	}
	if len(ops.Set)+len(ops.Unset) > maxBatchFieldOps {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d property operations per batch update", maxBatchFieldOps))
		return nil, false
	}
	load := func(raw string) (db.IssueProperty, bool) {
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("property id %q must be a UUID", raw))
			return db.IssueProperty{}, false
		}
		def, err := h.Queries.GetIssueProperty(r.Context(), db.GetIssuePropertyParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("property %s not found in this workspace", raw))
				return def, false
			}
			slog.Warn("batch update issues: load property", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to validate properties")
			return def, false
		}
		return def, true
	}

	plan := &batchPropertyPlan{}
	setIDs := make(map[pgtype.UUID]bool, len(ops.Set))
	rawIDs := make([]string, 0, len(ops.Set))
	for raw := range ops.Set {
		rawIDs = append(rawIDs, raw)
	}
	sort.Strings(rawIDs)
	for _, raw := range rawIDs {
		def, ok := load(raw)
		if !ok {
			return nil, false
		}
		if setIDs[def.ID] {
			continue
		}
		if def.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("property %q is archived and cannot receive new values", def.Name))
			return nil, false
		}
		value, err := validatePropertyValue(def, ops.Set[raw])
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("property %q: %s", def.Name, err.Error()))
			return nil, false
		}
		if propertyTypeIsActor(def.Type) {
			refs, err := actorRefsInValue(def.Type, value)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return nil, false
			}
			if status, msg := h.resolveActorRefs(r, uuidToString(workspaceID), refs); status != 0 {
				writeError(w, status, msg)
				return nil, false
			}
		}
		setIDs[def.ID] = true
		plan.set = append(plan.set, batchPropertySet{id: def.ID, raw: ops.Set[raw]})
		plan.lockKeys = append(plan.lockKeys, "prop:"+uuidToString(def.ID))
	}
	unsetIDs := make(map[pgtype.UUID]bool, len(ops.Unset))
	for _, raw := range ops.Unset {
		// Unsetting stays allowed on an archived definition: cleanup must never
		// be blocked, as in DeleteIssueProperty.
		def, ok := load(raw)
		if !ok {
			return nil, false
		}
		if setIDs[def.ID] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("property %q cannot be both set and unset", def.Name))
			return nil, false
		}
		if unsetIDs[def.ID] {
			continue
		}
		unsetIDs[def.ID] = true
		plan.unset = append(plan.unset, def.ID)
	}
	sort.Strings(plan.lockKeys)
	return plan, true
}

type batchIssueWrite struct {
	issue             db.Issue
	prev              db.Issue
	labelsChanged     bool
	propertiesChanged bool
}

// writeBatchIssue applies one issue's field update, label operations and
// property operations in a single transaction, so an issue is either written
// in full or skipped in full. params is nil when the batch changes no field.
func (h *Handler) writeBatchIssue(ctx context.Context, workspaceID pgtype.UUID, prev db.Issue, params *db.UpdateIssueParams, rawFields map[string]json.RawMessage, workflowAssignee bool, statusKey string, labels *batchLabelPlan, properties *batchPropertyPlan) (batchIssueWrite, error) {
	out := batchIssueWrite{issue: prev, prev: prev}
	tx, err := h.beginWakeupWrite(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	q := h.Queries.WithTx(tx)

	// Property locks first, then the status catalog lock, then row locks —
	// the order SetIssueProperty and the status guard each take theirs in.
	if properties != nil {
		for _, key := range properties.lockKeys {
			if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
				return out, err
			}
		}
	}
	if err := assertIssueStatusStillActive(ctx, q, workspaceID, statusKey); err != nil {
		return out, err
	}

	var cancelled []db.AgentTaskQueue
	if params != nil {
		out.prev, err = lockIssueForFieldUpdate(ctx, q, workspaceID, params, rawFields, workflowAssignee)
		if err != nil {
			return out, err
		}
		out.issue, cancelled, err = updateIssueStoppingWakeups(ctx, q, *params)
		if err != nil {
			return out, err
		}
	}
	if labels != nil {
		if out.labelsChanged, err = applyBatchLabelPlan(ctx, q, prev, labels); err != nil {
			return out, err
		}
	}
	if properties != nil {
		if out.propertiesChanged, err = applyBatchPropertyPlan(ctx, q, prev, properties); err != nil {
			return out, err
		}
	}
	if out.labelsChanged || out.propertiesChanged {
		if out.issue, err = q.GetIssue(ctx, prev.ID); err != nil {
			return out, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return out, err
	}
	h.broadcastCancelledWakeups(ctx, workspaceID, cancelled)
	return out, nil
}

func applyBatchLabelPlan(ctx context.Context, q *db.Queries, issue db.Issue, plan *batchLabelPlan) (bool, error) {
	remove := plan.remove
	if plan.clear {
		current, err := q.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return false, err
		}
		keep := make(map[pgtype.UUID]bool, len(plan.add))
		for _, id := range plan.add {
			keep[id] = true
		}
		remove = nil
		for _, label := range current {
			if !keep[label.ID] {
				remove = append(remove, label.ID)
			}
		}
	}
	changed := false
	for _, id := range remove {
		row, err := q.DetachLabelFromIssue(ctx, db.DetachLabelFromIssueParams{IssueID: issue.ID, LabelID: id, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return false, err
		}
		changed = changed || row.Changed
	}
	for _, id := range plan.add {
		row, err := q.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{IssueID: issue.ID, LabelID: id, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			return false, err
		}
		changed = changed || row.Changed
	}
	return changed, nil
}

func applyBatchPropertyPlan(ctx context.Context, q *db.Queries, issue db.Issue, plan *batchPropertyPlan) (bool, error) {
	// Value writes bump the revision only when the stored value changes.
	current, err := q.GetIssue(ctx, issue.ID)
	if err != nil {
		return false, err
	}
	revision := current.Revision
	changed := false
	note := func(updated db.Issue) {
		if updated.Revision != revision {
			changed = true
		}
		revision = updated.Revision
	}

	for _, s := range plan.set {
		def, err := q.GetIssueProperty(ctx, db.GetIssuePropertyParams{ID: s.id, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, &batchSkipError{code: "invalid_value", message: "the property was deleted while this request was in flight"}
			}
			return false, err
		}
		if def.ArchivedAt.Valid {
			return false, &batchSkipError{code: "invalid_value", message: fmt.Sprintf("property %q was archived while this request was in flight", def.Name)}
		}
		value, err := validatePropertyValue(def, s.raw)
		if err != nil {
			return false, &batchSkipError{code: "invalid_value", message: fmt.Sprintf("property %q: %s", def.Name, err.Error())}
		}
		updated, err := q.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Key:         uuidToString(def.ID),
			Value:       value,
		})
		if err != nil {
			if isCheckViolation(err) {
				return false, &batchSkipError{code: "properties_too_large", message: "issue properties would exceed the 16KB size limit"}
			}
			return false, err
		}
		note(updated)
	}
	for _, id := range plan.unset {
		updated, err := q.DeleteIssuePropertyValue(ctx, db.DeleteIssuePropertyValueParams{
			ID:          issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Key:         uuidToString(id),
		})
		if err != nil {
			return false, err
		}
		note(updated)
	}
	return changed, nil
}
