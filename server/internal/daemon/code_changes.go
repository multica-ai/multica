package daemon

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/codechanges"
)

type taskCodeChangeRepo struct {
	root, base string
	local      bool
}

// This object lives only in the existing active-task checkout window. Saved
// artifacts remain owned by the terminal report, not by a second repo registry.
type taskCodeChangeCapture struct {
	mu       sync.Mutex
	ref      codechanges.ExecutionRef
	ctx      context.Context
	overflow bool
	workDir  string
	env      *execenv.Environment
	repos    map[string]taskCodeChangeRepo
}

func prepareTaskCodeChanges(ctx context.Context, task Task, env *execenv.Environment) *taskCodeChangeCapture {
	if !task.CodeChangesEnabled || task.IssueID == "" || task.ChatSessionID != "" {
		return nil
	}
	ref := codechanges.ExecutionRef{RuntimeID: task.RuntimeID, DispatchedAt: task.DispatchedAt}
	if ref.Validate() != nil {
		return nil
	}
	capture := &taskCodeChangeCapture{ref: ref, ctx: ctx, workDir: env.WorkDir, env: env, repos: map[string]taskCodeChangeRepo{}}
	startCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if base, err := codechanges.Head(startCtx, env.WorkDir, "HEAD"); err == nil {
		capture.repos[env.WorkDir] = taskCodeChangeRepo{root: env.WorkDir, base: base, local: env.LocalWorktree != nil}
	}
	return capture
}
func (c *taskCodeChangeCapture) observeCheckout(root, base string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.repos[root]; !exists {
		if len(c.repos) >= codechanges.MaxFiles {
			c.overflow = true
			return
		}
		c.repos[root] = taskCodeChangeRepo{root: root, base: base}
	}
}
func (c *taskCodeChangeCapture) finish(result *TaskResult) {
	if c.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	defer cancel()
	c.mu.Lock()
	repos := make([]taskCodeChangeRepo, 0, len(c.repos))
	for _, repo := range c.repos {
		repos = append(repos, repo)
	}
	overflow := c.overflow
	c.mu.Unlock()
	sort.Slice(repos, func(i, j int) bool { return repos[i].root < repos[j].root })
	snapshot := &codechanges.Snapshot{Version: 1, Files: []codechanges.File{}, Truncated: overflow}
	if len(repos) == 0 {
		snapshot.Reason = "no_repository"
		result.CodeChanges = snapshot
		result.CodeChangesRef = c.ref
		return
	}
	remaining := codechanges.MaxDiffBytes
	for _, repo := range repos {
		root, ref, dirty := repo.root, "HEAD", true
		// A preserved worktree may contain runtime credentials committed before
		// cleanup refused delivery. Only Finalize's confirmed receipt is readable.
		if repo.local && result.DurableWorkDir == "" {
			snapshot.Reason = "capture_failed"
			snapshot.Truncated = true
			continue
		}
		if repo.local && result.DurableWorkDir != "" {
			root, ref, dirty = c.env.LocalWorktree.GitRoot, result.BranchName, false
			// Finalize removes a new branch when it contains no task changes.
			if ref == "" {
				ref = repo.base
			}
		}
		head, err := codechanges.Head(ctx, root, ref)
		if err != nil {
			snapshot.Reason = "capture_failed"
			snapshot.Truncated = true
			continue
		}
		captured := codechanges.Capture(ctx, root, repo.base, head, dirty)
		if snapshot.BaseRef == "" {
			snapshot.BaseRef, snapshot.HeadRef = captured.BaseRef, captured.HeadRef
		}
		if captured.Reason != "" {
			snapshot.Reason = captured.Reason
		}
		snapshot.WorkingTreeDirty = snapshot.WorkingTreeDirty || captured.WorkingTreeDirty
		snapshot.Truncated = snapshot.Truncated || captured.Truncated
		label := ""
		if len(repos) > 1 {
			label, _ = filepath.Rel(c.workDir, repo.root)
			label = filepath.ToSlash(label)
			if label == "." || strings.HasPrefix(label, "../") {
				label = filepath.Base(repo.root)
			}
		}
		for _, file := range captured.Files {
			if len(snapshot.Files) == codechanges.MaxFiles {
				snapshot.Truncated = true
				break
			}
			if label != "" {
				file.Path = label + "/" + file.Path
			}
			file.BaseRef, file.HeadRef = captured.BaseRef, captured.HeadRef
			if len(file.Diff) > remaining {
				file.Diff = strings.ToValidUTF8(file.Diff[:remaining], "?")
				file.Truncated = true
				snapshot.Truncated = true
			}
			remaining -= len(file.Diff)
			snapshot.Files = append(snapshot.Files, file)
		}
	}
	// JSON escaping has its own budget. An artifact must never block the actual
	// terminal result merely because a repository contains unusual text bytes.
	if _, err := codechanges.Encode(snapshot); err != nil {
		snapshot = &codechanges.Snapshot{Version: 1, Files: []codechanges.File{}, Reason: "capture_failed"}
	}
	if c.ctx.Err() != nil {
		return
	}
	result.CodeChanges = snapshot
	result.CodeChangesRef = c.ref
}
