package agent

import "strings"

// claudeTodoToolsEnv exposes Claude Code's checklist tools
// (TaskCreate/TaskUpdate/TaskList/TaskGet) in non-interactive `-p` runs, where
// they are off by default. The run transcript renders that checklist as the
// run's progress.
const claudeTodoToolsEnv = "CLAUDE_CODE_ENABLE_TODO_TOOLS"

// withClaudeTodoTools enables the checklist tools unless the environment
// already sets the switch, so a runtime or agent env of
// CLAUDE_CODE_ENABLE_TODO_TOOLS=0 still turns them off.
func withClaudeTodoTools(env []string) []string {
	for _, entry := range env {
		if key, _, _ := strings.Cut(entry, "="); key == claudeTodoToolsEnv {
			return env
		}
	}
	return append(env, claudeTodoToolsEnv+"=1")
}
