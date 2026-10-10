package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const (
	claudeNotificationResumeWindow = 10 * time.Second
	claudeNotificationReadLimit    = 1024 * 1024
)

// appendedTaskNotification reads only the requested session's new records.
// Historical notifications are common in healthy transcripts; their presence
// alone must never retire a session. Missing, replaced, truncated, oversized or
// unfamiliar transcripts provide no evidence and keep the resume pointer.
// This method never edits or logs transcript content.
func (s *claudeUsageSnapshot) appendedTaskNotification(sessionID string) (bool, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !os.SameFile(s.fileInfo, info) || info.Size() < s.offset || info.Size()-s.offset > claudeNotificationReadLimit {
		return false, nil
	}
	// Only consume complete JSONL records. A partial last line from before
	// launch must not be interpreted as a new provider event.
	if s.offset > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], s.offset-1); err != nil {
			return false, err
		}
		if last[0] != '\n' {
			return false, nil
		}
	}
	reader := bufio.NewReader(io.NewSectionReader(f, s.offset, info.Size()-s.offset))
	found := false
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return found && len(line) == 0, nil
		}
		if err != nil {
			return false, err
		}
		var record struct {
			Type      string          `json:"type"`
			Operation string          `json:"operation"`
			SessionID string          `json:"sessionId"`
			Content   json.RawMessage `json:"content"`
		}
		if json.Unmarshal(line, &record) != nil {
			return false, nil
		}
		// A persisted assistant turn also rules out an automatic replay, even
		// if a future CLI stops emitting it on stdout.
		if record.Type == "assistant" {
			return false, nil
		}
		if record.Type == "queue-operation" && record.Operation == "enqueue" && record.SessionID == sessionID {
			var content string
			if json.Unmarshal(record.Content, &content) == nil && strings.HasPrefix(strings.TrimSpace(content), "<task-notification>") {
				found = true
			}
		}
	}
}
