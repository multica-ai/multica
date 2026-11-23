package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// gateClaudeResumeToContextSize starts a fresh session instead of resuming one
// whose latest request already carried more than limit input tokens. Resuming
// such a session re-reads that whole context on the first call, and Claude Code
// soon compacts it, losing the detail the resume was meant to keep. limit is
// per-request input (input + cache read + cache creation of the latest
// assistant turn), not lifetime usage; limit <= 0 disables the check.
func gateClaudeResumeToContextSize(task *Task, taskCtx *execenv.TaskContextForEnv, provider, workDir string, childEnv map[string]string, limit int64, taskLog *slog.Logger) {
	if limit <= 0 || provider != "claude" || task.PriorSessionID == "" {
		return
	}
	sessionID := task.PriorSessionID
	if sessionID == "." || sessionID == ".." || strings.ContainsAny(sessionID, "/\\:\x00") {
		return
	}
	configDir, err := agent.ClaudeConfigDir(childEnv, workDir)
	if err != nil {
		return
	}
	path := filepath.Join(configDir, "projects", encodeClaudeCwd(workDir), sessionID+".jsonl")
	tokens, found, err := lastClaudeContextUsage(path)
	if err != nil || !found || tokens <= limit {
		return
	}
	taskLog.Info("dropping prior claude session: context exceeds resume limit; starting a fresh session",
		"session_id", sessionID, "context_tokens", tokens, "limit_tokens", limit)
	task.PriorSessionID = ""
	task.PriorSessionResumeUnavailable = true
	taskCtx.PriorSessionResumed = false
	taskCtx.PriorSessionResumeUnavailable = true
}

// lastClaudeContextUsage reads backwards in chunks so normal checks only read
// the end of a potentially large transcript. Stop at the latest assistant even
// if its usage is unavailable: older usage may predate a compaction.
func lastClaudeContextUsage(path string) (int64, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, false, err
	}
	const chunkSize = 64 * 1024
	var suffix []byte
	for position := info.Size(); position > 0; {
		start := max(position-chunkSize, 0)
		chunk := make([]byte, position-start)
		if _, err := f.ReadAt(chunk, start); err != nil {
			return 0, false, err
		}
		data := append(chunk, suffix...)
		lines := bytes.Split(data, []byte{'\n'})
		first := 0
		if start > 0 {
			suffix = bytes.Clone(lines[0])
			first = 1
		}
		for i := len(lines) - 1; i >= first; i-- {
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(line, &envelope); err != nil {
				return 0, false, err
			}
			if envelope.Type != "assistant" {
				continue
			}
			var entry struct {
				Message struct {
					Usage *struct {
						Input         *int64 `json:"input_tokens"`
						CacheRead     int64  `json:"cache_read_input_tokens"`
						CacheCreation int64  `json:"cache_creation_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal(line, &entry); err != nil {
				return 0, false, err
			}
			usage := entry.Message.Usage
			if usage == nil || usage.Input == nil || *usage.Input < 0 || usage.CacheRead < 0 || usage.CacheCreation < 0 {
				return 0, false, nil
			}
			return *usage.Input + usage.CacheRead + usage.CacheCreation, true, nil
		}
		position = start
	}
	return 0, false, nil
}
