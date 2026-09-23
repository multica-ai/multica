package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/agent"
)

type interactiveRunKey struct{}

func withInteractiveIssueRun(ctx context.Context, task Task, provider string) context.Context {
	if !interactiveIssueEnabled(task, provider) {
		return ctx
	}
	return context.WithValue(ctx, interactiveRunKey{}, task.RuntimeID)
}

func interactiveIssueEnabled(task Task, provider string) bool {
	if task.IssueID == "" || task.Agent == nil || (provider != "pi" && provider != "codex") {
		return false
	}
	var config struct {
		Enabled bool `json:"interactive_task_sessions"`
	}
	return json.Unmarshal(task.Agent.RuntimeConfig, &config) == nil && config.Enabled
}

// startInteractiveExecution binds the live process to the run's durable inbox.
// Polling is intentionally authoritative: losing a WebSocket notification must
// never lose a command. The 500ms poll also publishes observed pause state.
func (d *Daemon) startInteractiveExecution(ctx context.Context, backend agent.InteractiveBackend, prompt string, opts agent.ExecOptions, taskID, runtimeID string) (*agent.Session, error) {
	owner := uuid.NewString()
	deadline := time.Now().Add(23 * time.Hour)
	if opts.Timeout > 0 && time.Now().Add(opts.Timeout).Before(deadline) {
		deadline = time.Now().Add(opts.Timeout)
	}
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	path := fmt.Sprintf("/api/daemon/runtimes/%s/tasks/%s/interaction", runtimeID, taskID)
	var syncMu sync.Mutex
	syncRecord := func(request interaction.Sync) (interaction.Record, error) {
		syncMu.Lock()
		defer syncMu.Unlock()
		request.Owner = owner
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var record interaction.Record
		err := d.client.postJSON(callCtx, path, request, &record)
		return record, err
	}
	record, err := syncRecord(interaction.Sync{Register: true, Deadline: deadline, State: agent.InteractionSnapshot{State: agent.InteractionStarting, Activity: 1}})
	if err != nil {
		return nil, fmt.Errorf("register interactive run: %w", err)
	}
	opts.Timeout = time.Until(record.Deadline)
	opts.KeepInteractiveOpen = true
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("interactive run credential deadline expired")
	}
	ready := make(chan struct{})
	var session *agent.Session
	opts.InteractiveBeforeFinish = func() bool {
		select {
		case <-ready:
		case <-ctx.Done():
			return true
		}
		if session == nil || session.Control == nil {
			return true
		}
		record, err := syncRecord(interaction.Sync{Finish: true, State: session.Control.Snapshot()})
		return err == nil && record.Finishing
	}
	session, err = backend.ExecuteInteractive(ctx, prompt, opts)
	close(ready)
	if err != nil {
		return nil, err
	}
	if session.Control == nil {
		return nil, fmt.Errorf("interactive backend returned no controller")
	}
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		outcome := make(chan interaction.Input, 1)
		var ack *interaction.Input
		busy := false
		for {
			state := session.Control.Snapshot()
			if state.State == agent.InteractionFinished && !busy && ack == nil {
				return
			}
			record, err := syncRecord(interaction.Sync{State: state, Acknowledgement: ack})
			if err == nil {
				ack = nil
				if !busy {
					for _, command := range record.Commands {
						if command.Receipt != nil || command.Error != "" {
							continue
						}
						busy = true
						go func(command interaction.Input) {
							receipt, err := session.Control.Submit(ctx, command.InteractionCommand)
							if err != nil {
								command.Error = err.Error()
							} else {
								command.Receipt = &receipt
							}
							outcome <- command
						}(command)
						break
					}
				}
			}
			select {
			case result := <-outcome:
				ack, busy = &result, false
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	return session, nil
}
