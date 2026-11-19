package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func (d *Daemon) runCodexChatForkTask(ctx context.Context, task Task, provider string, cfg agent.Config) (TaskResult, error) {
	if provider != "codex" {
		return TaskResult{}, errors.New("native chat fork requires a Codex runtime")
	}
	fork := task.CodexChatFork
	if fork == nil || task.IssueID != "" || task.AgentID == "" ||
		task.ChatSessionID == "" || fork.SourceChatID == "" || fork.SourceChatID == task.ChatSessionID {
		return TaskResult{}, errors.New("Codex fork requires distinct source and destination chats")
	}

	sourceTask := execenv.TaskContextForEnv{
		AgentID: task.AgentID, ChatSessionID: fork.SourceChatID,
		RequireNativeResume: fork.SourceRequiresNativeResume,
	}
	sourceStore := execenv.CodexSessionStorePath(d.cfg.Profile, sourceTask)
	destinationStore := execenv.CodexSessionStorePath(d.cfg.Profile, execenv.TaskContextForEnv{
		AgentID: task.AgentID, ChatSessionID: task.ChatSessionID,
	})
	if sourceStore == "" || destinationStore == "" || sourceStore == destinationStore {
		return TaskResult{}, errors.New("Codex fork stores are unavailable or overlap")
	}
	stores := []string{sourceStore, destinationStore}
	sort.Strings(stores)
	for _, store := range stores {
		d.markActiveStore(store)
		defer d.unmarkActiveStore(store)
		lock, err := execenv.AcquireCodexSessionWriterLock(ctx, store, nil)
		if err != nil {
			return TaskResult{}, err
		}
		defer lock.Release()
	}

	childID, err := execenv.LookupPublishedCodexFork(destinationStore, task.ID)
	if err != nil {
		return TaskResult{}, err
	}
	var source execenv.CodexForkSource
	if childID == "" {
		var ownedEnvRoot string
		if fork.SourceWorkDir != "" && !fork.SourceRequiresNativeResume {
			sourceClaim := Task{ID: task.ID, WorkspaceID: task.WorkspaceID, AgentID: task.AgentID,
				ChatSessionID: fork.SourceChatID, PriorWorkDir: fork.SourceWorkDir}
			claim, workDir, _, ok, err := d.lockReusablePriorEnvRoot(ctx, sourceClaim, nil, "")
			if err != nil {
				return TaskResult{}, err
			}
			if ok {
				defer claim.Release()
				ownedEnvRoot = filepath.Dir(workDir)
				defer d.markActiveEnvRoot(ownedEnvRoot)()
			}
		}
		source, err = execenv.ResolveCodexForkSource(d.cfg.Profile, sourceTask, ownedEnvRoot, fork.SourceSessionID)
		if err != nil {
			return TaskResult{}, err
		}
	}

	if _, err := d.client.StartTask(ctx, task); err != nil {
		return TaskResult{}, fmt.Errorf("start Codex fork task: %w", err)
	}
	if childID != "" {
		return TaskResult{Status: "completed", SessionID: childID}, nil
	}
	cwd, err := os.MkdirTemp("", "multica-codex-fork-cwd-")
	if err != nil {
		return TaskResult{}, fmt.Errorf("create private Codex fork work directory: %w", err)
	}
	defer os.RemoveAll(cwd)
	childID, err = execenv.ForkCodexChat(ctx, cfg, cwd, source.SessionsDir, source.ArchiveDir, source.ConfigPath,
		fork.SourceSessionID, destinationStore, task.ID)
	if err != nil {
		return TaskResult{}, err
	}
	return TaskResult{Status: "completed", SessionID: childID}, nil
}
