package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/runcontrol"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// checkControllerAdmission runs inside StartTask's profile-locked transaction.
// The named budget lock spans profiles and daemons sharing that budget. Parked
// and queued rows do not reserve execution capacity; every wake checks again.
func (s *TaskService) checkControllerAdmission(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) error {
	if !task.IssueID.Valid {
		return nil
	}
	owned, err := q.ControllerOwnsIssue(ctx, task.IssueID)
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}
	if s.TxStarter == nil {
		return errors.New("controller admission requires a transaction")
	}
	row, err := q.LockControllerAdmission(ctx, task.ID)
	if err != nil {
		return errors.New("run has no controller authority")
	}
	var m runcontrol.Manifest
	var p struct {
		MaxActive       int       `json:"max_active"`
		BudgetExpiresAt time.Time `json:"budget_expires_at"`
	}
	if json.Unmarshal(row.Manifest, &m) != nil || json.Unmarshal(row.Config, &p) != nil {
		return errors.New("invalid controller authority")
	}
	if err = m.Validate(); err != nil {
		return err
	}
	profile, err := q.GetAgent(ctx, task.AgentID)
	if err != nil {
		return err
	}
	hash, err := ControllerProfileHash(ctx, q, profile)
	if err != nil {
		return err
	}
	if hash != m.ProfileHash {
		return errors.New("profile inputs changed after claim")
	}
	if row.IsStopped || row.AuthorityEpoch != m.AuthorityEpoch || row.ScopeRevision != m.ScopeRevision {
		return errors.New("controller authority revoked")
	}
	if time.Now().Before(m.NotBefore) {
		return fmt.Errorf("not eligible before %s", m.NotBefore)
	}
	if p.MaxActive < 1 || !p.BudgetExpiresAt.After(time.Now()) {
		return errors.New("controller host/provider budget expired")
	}
	if err = q.LockControllerBudget(ctx, m.WorkspaceID+":"+m.BudgetKey); err != nil {
		return err
	}
	workspace, err := util.ParseUUID(m.WorkspaceID)
	if err != nil {
		return err
	}
	count, err := q.CountControllerBudget(ctx, db.CountControllerBudgetParams{WorkspaceID: workspace, BudgetKey: m.BudgetKey, TaskID: task.ID})
	if err != nil {
		return err
	}
	if count >= int64(p.MaxActive) {
		return fmt.Errorf("budget %s: %w", m.BudgetKey, ErrTaskExecutionCapacity)
	}
	return nil
}
