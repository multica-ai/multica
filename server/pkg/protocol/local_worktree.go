package protocol

// LocalWorktreeResumeSource binds a prior session cwd to a finalized local
// worktree on the claiming runtime. RootTaskID identifies the task that first
// allocated the cwd, even when later rounds recorded the same path.
type LocalWorktreeResumeSource struct {
	RootTaskID string `json:"root_task_id"`
	LocalPath  string `json:"local_path"`
	Branch     string `json:"branch,omitempty"`
}
