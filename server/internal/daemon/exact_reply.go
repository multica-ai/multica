package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const exactReplyUnavailable = "Required handler receipt is unavailable; task completion is not confirmed."

// The opt-in is read from the server-owned agent configuration. Custom paths
// remain blocked; each chat task receives a fresh daemon-owned receipt path.
func prepareExactReply(task Task, root string, environment map[string]string) (string, error) {
	if task.ChatSessionID == "" || task.Agent == nil || task.Agent.CustomEnv["MULTICA_EXACT_REPLY_REQUIRED"] != "1" {
		return "", nil
	}
	file, err := os.CreateTemp(root, "exact-reply-*")
	if err != nil {
		return "", fmt.Errorf("prepare exact reply: %w", err)
	}
	path := file.Name()
	_, writeErr := file.WriteString("MULTICA_EXACT_REPLY/1 state=idle\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return "", fmt.Errorf("initialize exact reply: %w", errors.Join(writeErr, closeErr))
	}
	environment["MULTICA_EXACT_REPLY_REQUIRED"] = "1"
	environment["MULTICA_EXACT_REPLY_FILE"] = path
	return path, nil
}

// This runs at the terminal task boundary, after provider result conversion.
// Handler files authorize output, never a transition from failure to success.
func enforceExactReply(result TaskResult, path string) TaskResult {
	if path == "" || result.Status == "cancelled" {
		return result
	}
	receipt, ok := readExactReply(path)
	if ok && result.Status == "completed" {
		result.Comment = receipt
		return result
	}
	result.Status = "blocked"
	result.Comment = exactReplyUnavailable
	result.FailureReason = "agent_error.unknown"
	return result
}

func readExactReply(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 || !utf8.Valid(data) {
		return "", false
	}
	text := string(data)
	if text == "MULTICA_EXACT_REPLY/1 state=provider\n" {
		return "", true
	}
	if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "MULTICA_EXACT_REPLY/") {
		return "", false
	}
	return text, true
}
