package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	defaultLongReplyThreshold = 7000
	maxLongReplyThreshold     = 7999
)

// offloadLongReply runs before terminal-report persistence, so completion retries
// reuse the attachment rather than uploading again. Do not post a CLI comment:
// delivery must remain on the server's synthesized agent-comment path.
func (d *Daemon) offloadLongReply(ctx context.Context, task Task, result TaskResult, taskLog *slog.Logger) TaskResult {
	if !boolFromEnv("MULTICA_LONG_REPLY_ATTACHMENTS", false) || result.Status != "completed" || task.IssueID == "" || task.ChatSessionID != "" {
		return result
	}

	threshold, err := intFromEnv("MULTICA_LONG_REPLY_THRESHOLD", defaultLongReplyThreshold)
	if err != nil || threshold <= 0 {
		threshold = defaultLongReplyThreshold
	}
	// Stay below the server's 8000-rune synthesized-comment cap even when
	// configured incorrectly. The replacement itself has a fixed small excerpt.
	if threshold > maxLongReplyThreshold {
		threshold = maxLongReplyThreshold
	}
	body := redact.Text(util.DecodeEscapedAgentOutput(result.Comment))
	if utf8.RuneCountInString(body) <= threshold {
		return result
	}

	token, err := taskScopedAuthToken(task)
	if err != nil {
		taskLog.Warn("long reply attachment skipped", "error", err)
		return result
	}
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filename := "reply-" + shortID(task.ID) + ".md"
	client := cli.NewAPIClient(d.cfg.ServerBaseURL, task.WorkspaceID, token)
	attachment, err := client.UploadIssueAttachment(uploadCtx, []byte(body), filename, task.IssueID)
	if err != nil || attachment.MarkdownURL == "" {
		taskLog.Warn("long reply attachment unavailable; reporting original output", "error", err)
		return result
	}

	// A quoted excerpt contains any partial Markdown fence inside the quote,
	// keeping the attachment card outside it and visible.
	excerpt := []rune(strings.TrimSpace(body))
	if len(excerpt) > 300 {
		excerpt = append(excerpt[:300], '…')
	}
	quote := "> " + strings.ReplaceAll(string(excerpt), "\n", "\n> ")
	result.Comment = fmt.Sprintf("%s\n\nFull reply attached as Markdown (%d characters).\n\n!file[%s](%s)", quote, utf8.RuneCountInString(body), filename, attachment.MarkdownURL)
	return result
}
