package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/localenv"
)

// Environment commands come from the issue sidebar: a member stops, restarts
// or starts an environment, or reads its log. The server queues them and
// sends a pending-work hint; the daemon claims them, carries them out and
// reports each result.

const (
	environmentLogLines       = 500
	environmentLogBytes       = 256 * 1024
	environmentCommandTimeout = 90 * time.Second
	environmentConfigsTimeout = 15 * time.Second
)

type environmentController interface {
	ClaimEnvironmentCommands(ctx context.Context, runtimeID string) ([]EnvironmentCommand, error)
	ReportEnvironmentCommandResult(ctx context.Context, runtimeID, commandID string, result EnvironmentCommandResult) error
	ReportTaskEnvironmentConfigs(ctx context.Context, taskID string, envs []localenv.Declared) error
}

// claimCommands carries out the commands queued for a runtime. One claim runs
// per runtime at a time; a hint that arrives meanwhile makes it claim again
// when it is done.
func (s *environmentService) claimCommands(ctx context.Context, runtimeID string) {
	if s == nil {
		return
	}
	ctl, ok := s.client.(environmentController)
	if !ok {
		return
	}
	s.mu.Lock()
	if s.claiming[runtimeID] {
		s.claimAgain[runtimeID] = true
		s.mu.Unlock()
		return
	}
	s.claiming[runtimeID] = true
	s.mu.Unlock()
	for {
		cmds, err := ctl.ClaimEnvironmentCommands(ctx, runtimeID)
		if err != nil && ctx.Err() == nil {
			s.logger.Warn("environments: claim commands failed", "runtime_id", runtimeID, "error", err)
		}
		for _, cmd := range cmds {
			go s.execute(ctx, ctl, runtimeID, cmd)
		}
		s.mu.Lock()
		again := s.claimAgain[runtimeID]
		delete(s.claimAgain, runtimeID)
		if !again || ctx.Err() != nil {
			delete(s.claiming, runtimeID)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}

func (s *environmentService) execute(ctx context.Context, ctl environmentController, runtimeID string, cmd EnvironmentCommand) {
	runCtx, cancel := context.WithTimeout(ctx, environmentCommandTimeout)
	result := s.carryOut(runCtx, cmd)
	cancel()
	if !result.OK {
		s.logger.Info("environments: command failed", "action", cmd.Action, "command_id", cmd.ID, "error", result.Error)
	}
	reportCtx, cancelReport := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelReport()
	if err := ctl.ReportEnvironmentCommandResult(reportCtx, runtimeID, cmd.ID, result); err != nil {
		s.logger.Warn("environments: report command result failed", "command_id", cmd.ID, "error", err)
	}
	if cmd.Action != "logs" {
		s.nudge()
	}
}

func (s *environmentService) carryOut(ctx context.Context, cmd EnvironmentCommand) EnvironmentCommandResult {
	fail := func(err error) EnvironmentCommandResult {
		return EnvironmentCommandResult{Error: err.Error()}
	}
	id, declared := localenv.IDFromKey(cmd.LocalKey)
	if declared {
		issueID, workspaceID, err := s.manager.RecordOwner(id)
		if err != nil {
			return fail(err)
		}
		if issueID != cmd.IssueID || workspaceID != cmd.WorkspaceID {
			return fail(localenv.ErrUnknown)
		}
	}
	switch cmd.Action {
	case "stop":
		if !declared {
			return s.stopDetected(ctx, cmd)
		}
		if _, err := s.manager.StopID(id); err != nil {
			return fail(err)
		}
		return EnvironmentCommandResult{OK: true}
	case "restart":
		if !declared {
			return fail(errors.New("only environments the repository declares can be restarted"))
		}
		env, err := s.manager.Restart(id, s.resolve)
		if err != nil {
			return fail(err)
		}
		return EnvironmentCommandResult{OK: true, EnvironmentKey: env.Key}
	case "start":
		env, err := s.startDeclared(cmd)
		if err != nil {
			return fail(err)
		}
		return EnvironmentCommandResult{OK: true, EnvironmentKey: env.Key}
	case "logs":
		if !declared {
			return fail(errors.New("only environments the repository declares have a log"))
		}
		out, err := s.manager.LogTail(id, environmentLogLines, environmentLogBytes)
		if err != nil {
			return fail(err)
		}
		return EnvironmentCommandResult{OK: true, Output: &out}
	}
	return fail(errors.New("this machine does not know the command " + cmd.Action))
}

// stopDetected stops a server detection found in the issue's directory. One
// that is no longer detected has already stopped.
func (s *environmentService) stopDetected(ctx context.Context, cmd EnvironmentCommand) EnvironmentCommandResult {
	var target *localenv.Environment
	s.mu.Lock()
	for i := range s.detected {
		if s.detected[i].Key == cmd.LocalKey && s.detected[i].IssueID == cmd.IssueID {
			env := s.detected[i]
			target = &env
			break
		}
	}
	s.mu.Unlock()
	if target != nil {
		if err := s.detector.Stop(ctx, *target); err != nil {
			return EnvironmentCommandResult{Error: err.Error()}
		}
	}
	// Detect now, so the report this triggers no longer lists it.
	s.detect(ctx)
	return EnvironmentCommandResult{OK: true}
}

// startDeclared starts an environment the task's repositories declare, in the
// directory the task ran in, or the issue's latest one if that was reused.
func (s *environmentService) startDeclared(cmd EnvironmentCommand) (localenv.Environment, error) {
	task, ok := s.taskRoot(cmd.WorkspaceID, cmd.IssueID, cmd.TaskID)
	if !ok {
		return localenv.Environment{}, localenv.ErrGone
	}
	for _, d := range localenv.DiscoverDeclared(filepath.Join(task.RootDir, "workdir")) {
		if d.Repo == cmd.Repo && d.Name == cmd.Name {
			path := s.manager.LastPath(task.IssueID, task.WorkspaceID, d.Name)
			return s.manager.Start(task, d.RepoRoot, d.Name, path)
		}
	}
	return localenv.Environment{}, errors.New("the latest code on this machine no longer declares this environment")
}

// taskRoot finds the directory a task ran in: a running task's, else the one
// whose .gc_meta.json names it, else the issue's most recent one.
func (s *environmentService) taskRoot(workspaceID, issueID, taskID string) (localenv.Task, bool) {
	s.mu.Lock()
	for root, t := range s.tasks {
		if t.TaskID == taskID && t.IssueID == issueID {
			s.mu.Unlock()
			t.RootDir = root
			return t, true
		}
	}
	s.mu.Unlock()

	var best localenv.Task
	var bestAt time.Time
	for _, wsDir := range environmentSubdirs(s.workspacesRoot) {
		for _, root := range environmentSubdirs(wsDir) {
			meta, err := execenv.ReadGCMeta(root)
			if err != nil || meta.Kind != execenv.GCKindIssue || meta.IssueID != issueID ||
				meta.WorkspaceID != workspaceID || meta.LocalDirectory {
				continue
			}
			task := localenv.Task{TaskID: meta.TaskID, IssueID: meta.IssueID, WorkspaceID: meta.WorkspaceID, RootDir: root}
			if meta.TaskID == taskID {
				return task, true
			}
			if best.RootDir == "" || meta.CompletedAt.After(bestAt) {
				best, bestAt = task, meta.CompletedAt
			}
		}
	}
	return best, best.RootDir != ""
}

func environmentSubdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// reportDeclared tells the server what a finished task's repositories
// declare, so the sidebar can offer to start them from its directory.
func (s *environmentService) reportDeclared(ctx context.Context, taskID, envRoot string) {
	if s == nil || taskID == "" || envRoot == "" {
		return
	}
	ctl, ok := s.client.(environmentController)
	if !ok {
		return
	}
	declared := localenv.DiscoverDeclared(filepath.Join(envRoot, "workdir"))
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), environmentConfigsTimeout)
	defer cancel()
	if err := ctl.ReportTaskEnvironmentConfigs(reportCtx, taskID, declared); err != nil {
		s.logger.Debug("environments: report declared environments failed", "task_id", taskID, "error", err)
	}
}
