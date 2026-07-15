package apps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/workflowexec"
)

func (h *Handler) runWorkflow(ctx context.Context, runID uuid.UUID, definition json.RawMessage, trigger any, identity tokens.Identity) (workflowexec.Result, error) {
	identity.App.RunID = runID.String()
	tokenSource := workflowTokenSource{issuer: h.tokens, identity: identity}
	registry := newRegistryAdapter(os.Getenv("FIRTAL_REGISTRY_URL"), runID.String(), nil)
	views := &databaseViewWaiter{
		pool:       h.pool,
		runID:      runID,
		appID:      identity.App.ID,
		appVersion: identity.App.Version,
		memberID:   identity.MemberID,
		target:     extractViewTarget(trigger),
	}
	return workflowexec.New(tokenSource, registry, views).Run(ctx, definition, trigger)
}

type databaseViewWaiter struct {
	pool       *pgxpool.Pool
	runID      uuid.UUID
	appID      string
	appVersion string
	memberID   string
	target     viewTarget
	pollEvery  time.Duration
}

type viewTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func extractViewTarget(trigger any) viewTarget {
	raw, err := json.Marshal(trigger)
	if err != nil {
		return viewTarget{}
	}
	var envelope struct {
		Target viewTarget `json:"_multica_target"`
	}
	_ = json.Unmarshal(raw, &envelope)
	return envelope.Target
}

func (w *databaseViewWaiter) ShowAndWait(ctx context.Context, stepID, viewID string, input any) (any, error) {
	if w == nil || w.pool == nil {
		return nil, errors.New("workflow view store is not configured")
	}
	if w.pollEvery <= 0 {
		w.pollEvery = 500 * time.Millisecond
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var requestID uuid.UUID
	err = w.pool.QueryRow(ctx, `
		INSERT INTO cerebro_app_view_request (workflow_run_id,step_id,app_id,app_version,view_id,input)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (workflow_run_id,step_id) DO UPDATE SET input=EXCLUDED.input
		RETURNING id`, w.runID, stepID, w.appID, w.appVersion, viewID, inputJSON).Scan(&requestID)
	if err != nil {
		return nil, err
	}
	_, _ = w.pool.Exec(ctx, `UPDATE cerebro_app_workflow_run SET status='waiting' WHERE id=$1 AND status='running'`, w.runID)
	if err := w.publishCard(ctx, requestID); err != nil {
		return nil, err
	}
	for {
		var output json.RawMessage
		err = w.pool.QueryRow(ctx, `SELECT output FROM cerebro_app_view_request WHERE id=$1 AND status='submitted'`, requestID).Scan(&output)
		if err == nil {
			_, _ = w.pool.Exec(ctx, `UPDATE cerebro_app_workflow_run SET status='running' WHERE id=$1 AND status='waiting'`, w.runID)
			var value any
			if decodeErr := json.Unmarshal(output, &value); decodeErr != nil {
				return nil, decodeErr
			}
			return value, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		timer := time.NewTimer(w.pollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *databaseViewWaiter) publishCard(ctx context.Context, requestID uuid.UUID) error {
	if w.target.Kind == "" || w.target.ID == "" {
		return nil
	}
	targetID, err := uuid.Parse(w.target.ID)
	if err != nil {
		return errors.New("interactive view target is invalid")
	}
	content := "[Open interactive app](mention://app-view/" + requestID.String() + ")"
	switch w.target.Kind {
	case "chat":
		result, err := w.pool.Exec(ctx, `
			INSERT INTO chat_message (chat_session_id,role,content)
			SELECT cs.id,'assistant',$2 FROM chat_session cs
			JOIN cerebro_app_workflow_def wd ON wd.workspace_id=cs.workspace_id
			JOIN cerebro_app_workflow_run wr ON wr.workflow_id=wd.id
			WHERE cs.id=$1 AND wr.id=$3`, targetID, content, w.runID)
		if err != nil || result.RowsAffected() != 1 {
			return errors.New("interactive chat target is unavailable")
		}
		return nil
	case "issue":
		memberID, parseErr := uuid.Parse(w.memberID)
		if parseErr != nil {
			return errors.New("interactive view member is invalid")
		}
		result, err := w.pool.Exec(ctx, `
			INSERT INTO comment (issue_id,workspace_id,author_type,author_id,content)
			SELECT i.id,i.workspace_id,'member',$2,$3 FROM issue i
			JOIN cerebro_app_workflow_def wd ON wd.workspace_id=i.workspace_id
			JOIN cerebro_app_workflow_run wr ON wr.workflow_id=wd.id
			WHERE i.id=$1 AND wr.id=$4`, targetID, memberID, content, w.runID)
		if err != nil || result.RowsAffected() != 1 {
			return errors.New("interactive issue target is unavailable")
		}
		return nil
	default:
		return errors.New("interactive view target kind is unsupported")
	}
}
