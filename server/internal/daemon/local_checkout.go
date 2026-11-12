package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

var errLocalCheckoutClosed = errors.New("local worktree checkout is closed: this run has ended")

// localCheckout owns request admission as well as the worktree. Authenticating
// a request can race token removal, so callers must also pass this run's gate.
// Environment.LocalWorktree is published only by the run goroutine after close,
// keeping provider execution and finalization free of concurrent pointer writes.
type localCheckout struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	prepare  func(context.Context) (*execenv.LocalWorktree, error)
	pending  chan struct{}
	worktree *execenv.LocalWorktree
	closed   bool
}

func newLocalCheckout(ctx context.Context, prepare func(context.Context) (*execenv.LocalWorktree, error)) *localCheckout {
	ctx, cancel := context.WithCancel(ctx)
	return &localCheckout{ctx: ctx, cancel: cancel, prepare: prepare}
}

func (s *localCheckout) checkout(ctx context.Context) (*execenv.LocalWorktree, error) {
	for {
		s.mu.Lock()
		if s.closed || s.ctx.Err() != nil {
			s.mu.Unlock()
			return nil, errLocalCheckoutClosed
		}
		if s.worktree != nil {
			wt := s.worktree
			s.mu.Unlock()
			return wt, nil
		}
		if pending := s.pending; pending != nil {
			s.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-s.ctx.Done():
				return nil, errLocalCheckoutClosed
			}
		}
		s.pending = make(chan struct{})
		s.mu.Unlock()

		prepareCtx, cancel := context.WithCancel(s.ctx)
		stop := context.AfterFunc(ctx, cancel)
		wt, err := s.prepare(prepareCtx)
		stop()
		cancel()
		s.mu.Lock()
		if wt != nil {
			s.worktree = wt
		}
		close(s.pending)
		s.pending = nil
		s.mu.Unlock()
		return wt, err
	}
}

func (s *localCheckout) close() *execenv.LocalWorktree {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	s.cancel()
	pending := s.pending
	s.mu.Unlock()
	if pending != nil {
		<-pending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.worktree
}

func (d *Daemon) taskLocalCheckout(ctx context.Context, task Task, assignment *localDirectoryAssignment, env *execenv.Environment) *localCheckout {
	if !assignment.UsesWorktree() {
		return nil
	}
	params := execenv.LocalWorktreeParamsForRun(execenv.PrepareParams{
		WorkspaceID: task.WorkspaceID, TaskID: task.ID, AgentName: task.Agent.Name,
		IssueIdentifier: task.IssueIdentifier,
		Task:            execenv.TaskContextForEnv{AgentID: task.AgentID, IssueID: task.IssueID, ChatSessionID: task.ChatSessionID},
	}, env.RootDir)
	params.LocalPath = assignment.AbsPath
	params.PreserveExisting = true
	return newLocalCheckout(ctx, func(ctx context.Context) (*execenv.LocalWorktree, error) {
		ctx, cancel := context.WithTimeout(ctx, d.effectiveTaskPrepareTimeout())
		defer cancel()
		release, err := d.localPathLocks.Acquire(ctx, assignment.RealPath, task.ID, func(holder string) {
			d.logger.Info("local_directory: on-demand snapshot waiting for holder", "task_id", task.ID, "holder", holder)
		})
		if err != nil {
			return nil, err
		}
		defer release()
		if d.executionEnvironmentCommand == nil {
			return execenv.PrepareLocalWorktree(params, d.logger)
		}
		command, err := d.executionEnvironmentCommand()
		if err != nil {
			return nil, err
		}
		return execenv.PrepareLocalWorktreeIsolated(ctx, command, params, d.logger)
	})
}

func (d *Daemon) localCheckoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		task, auth := d.activeRepoCheckoutTask(r)
		if auth != repoCheckoutAuthOK {
			d.writeRepoCheckoutAuthError(w, auth)
			return
		}
		if task.LocalCheckout == nil {
			http.Error(w, "this task has no local directory in parallel mode", http.StatusConflict)
			return
		}
		wt, err := task.LocalCheckout.checkout(r.Context())
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errLocalCheckoutClosed) {
				status = http.StatusGone
			}
			http.Error(w, err.Error(), status)
			return
		}
		description := fmt.Sprintf("Local worktree: %s\nUser changes captured at %s (uncommitted changes included: %t). Recheck earlier observations against this snapshot.\nRead this repository's AGENTS.md / CLAUDE.md and other instructions before editing.\n", wt.WorkDir, wt.SnapshotAt.Format(time.RFC3339), wt.DirtyBaseCaptured)
		description += buildWorktreeReplayConflictBlock(wt.ReplayConflicts)
		description += buildWorktreeBranchBlock(wt.Branch, wt.BaseCommit, wt.CommitsAllLeftovers())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Path        string `json:"path"`
			Branch      string `json:"branch_name"`
			Description string `json:"description"`
		}{wt.WorkDir, wt.Branch, description})
	}
}
