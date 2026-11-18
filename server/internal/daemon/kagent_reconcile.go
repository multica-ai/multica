package daemon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

type kagentRuntimeEnrollment struct {
	AgentID      string `json:"agent_id"`
	Name         string `json:"name"`
	WorkspaceID  string `json:"workspace_id"`
	RuntimeID    string `json:"runtime_id"`
	Revision     int64  `json:"revision"`
	Generation   int64  `json:"generation"`
	DesiredState string `json:"desired_state"`
	Phase        string `json:"phase"`
}

func (c *Client) listKagentEnrollments(ctx context.Context, runtimeID string) ([]kagentRuntimeEnrollment, error) {
	var rows []kagentRuntimeEnrollment
	err := c.getJSON(ctx, "/api/daemon/runtimes/"+url.PathEscape(runtimeID)+"/kagent/agents", &rows)
	return rows, err
}

func (c *Client) reportKagentPhase(ctx context.Context, row *kagentRuntimeEnrollment, phase string) error {
	if row.Revision <= 0 || row.Revision == math.MaxInt64 {
		return fmt.Errorf("kagent: invalid readiness revision")
	}
	var result kagentRuntimeEnrollment
	err := c.postJSON(ctx, "/api/daemon/runtimes/"+url.PathEscape(row.RuntimeID)+"/kagent/agents/"+url.PathEscape(row.AgentID)+"/report", struct {
		ExpectedRevision   int64  `json:"expected_revision"`
		ExpectedGeneration int64  `json:"expected_generation"`
		ExpectedPhase      string `json:"expected_phase"`
		Phase              string `json:"phase"`
	}{row.Revision, row.Generation, row.Phase, phase}, &result)
	if err != nil {
		return err
	}
	expected := *row
	expected.Revision++
	expected.Phase = phase
	if result != expected {
		return fmt.Errorf("kagent: readiness response changed binding")
	}
	*row = result
	return nil
}

// Runtime IDs come only from this daemon's registered workspace set.
func (d *Daemon) kagentRuntimeScopes() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	scopes := map[string]string{}
	for workspaceID, ws := range d.workspaces {
		for _, id := range ws.runtimeIDs {
			if rt, ok := d.runtimeIndex[id]; ok && rt.Provider == "kagent" && rt.ID == id {
				scopes[id] = workspaceID
			}
		}
	}
	return scopes
}

func validKagentEnrollment(c kagentDaemonConfig, row kagentRuntimeEnrollment, runtimeID, workspaceID string) bool {
	if row.RuntimeID != runtimeID || row.WorkspaceID != workspaceID || !c.allows(row.AgentID, workspaceID) || row.Generation <= 0 || row.Revision <= 0 || row.DesiredState != "present" || row.Name == "" {
		return false
	}
	if _, err := kagent.ResourceName(row.AgentID); err != nil {
		return false
	}
	return row.Phase == "pending" || row.Phase == "ready" || row.Phase == "blocked"
}

// No cached approval: a task must still be present and ready on the authenticated
// server. Provisioning belongs to the periodic loop, never to a task claim.
func (d *Daemon) authorizeKagentTask(ctx context.Context, task Task) error {
	if err := validateKagentTask(task); err != nil {
		return err
	}
	cfg, err := loadKagentDaemonConfig()
	if err != nil {
		return err
	}
	if d.client == nil || d.kagentRuntimeScopes()[task.RuntimeID] != task.WorkspaceID {
		return fmt.Errorf("kagent: runtime is not registered in workspace")
	}
	rows, err := d.client.listKagentEnrollments(ctx, task.RuntimeID)
	if err != nil {
		return fmt.Errorf("kagent: enrollment authorization unavailable")
	}
	matches := 0
	for _, row := range rows {
		if row.AgentID == task.AgentID {
			if !validKagentEnrollment(cfg, row, task.RuntimeID, task.WorkspaceID) || row.Phase != "ready" || row.Name != task.Agent.Name {
				return fmt.Errorf("kagent: task enrollment mismatch")
			}
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("kagent: task requires one approved server enrollment")
	}
	return nil
}

func (d *Daemon) kagentReconcileRound(ctx context.Context) error {
	cfg, err := loadKagentDaemonConfig()
	if err != nil {
		return err
	}
	lifecycle, _, err := cfg.lifecycle()
	if err != nil {
		return err
	}
	var roundErr error
	for runtimeID, workspaceID := range d.kagentRuntimeScopes() {
		rows, err := d.client.listKagentEnrollments(ctx, runtimeID)
		if err != nil {
			roundErr = errors.Join(roundErr, fmt.Errorf("kagent: enrollment discovery unavailable"))
			continue
		}
		counts := map[string]int{}
		for _, row := range rows {
			counts[row.AgentID]++
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if counts[row.AgentID] != 1 || !validKagentEnrollment(cfg, row, runtimeID, workspaceID) {
				continue
			}
			// A failed CAS ends this attempt. The next round reloads server state;
			// neither a stale snapshot nor an absent enrollment authorizes cleanup.
			if row.Phase == "blocked" {
				if err := d.client.reportKagentPhase(ctx, &row, "pending"); err != nil {
					roundErr = errors.Join(roundErr, fmt.Errorf("kagent: readiness CAS failed"))
					continue
				}
			}
			err := lifecycle.Enroll(kagent.Desired{AgentID: row.AgentID, AgentName: row.Name, Namespace: cfg.Namespace, HarnessRefName: cfg.Harness,
				Inline: &kagent.AgentTemplateSpec{ModelConfig: &kagent.LocalObjectReference{Name: cfg.ModelConfig}, SystemPrompt: "Native text DEV agent. No local checkout or Multica tools are available."}})
			var binding kagent.Binding
			if err == nil {
				binding, err = lifecycle.Reconcile(ctx, row.AgentID)
			}
			// Active executions hold the same journal lock. Busy is not evidence of
			// readiness drift and must not interfere with their outcome fences.
			if errors.Is(err, kagent.ErrBusy) {
				roundErr = errors.Join(roundErr, err)
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			phase := "blocked"
			if err == nil && binding.Outcome.Ready {
				phase = "ready"
			}
			if err != nil || phase == "blocked" {
				roundErr = errors.Join(roundErr, fmt.Errorf("kagent: reconciliation not ready"))
			}
			if phase != row.Phase {
				if err := d.client.reportKagentPhase(ctx, &row, phase); err != nil {
					roundErr = errors.Join(roundErr, fmt.Errorf("kagent: readiness CAS failed"))
				}
			}
		}
	}
	return roundErr
}

func (d *Daemon) kagentReconcileLoop(ctx context.Context, minimum, maximum time.Duration) {
	delay := minimum
	for ctx.Err() == nil {
		// Opt-in is checked every round, including after startup/config repair.
		if os.Getenv("MULTICA_KAGENT_ENABLED") == "1" {
			round, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := d.kagentReconcileRound(round)
			cancel()
			if err != nil {
				// Fixed text only: remote errors and credentials never enter reports/logs.
				if d.logger != nil {
					d.logger.Debug("kagent readiness reconciliation will retry")
				}
				delay *= 2
				if delay > maximum {
					delay = maximum
				}
			} else {
				delay = minimum
			}
		} else {
			delay = minimum
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
