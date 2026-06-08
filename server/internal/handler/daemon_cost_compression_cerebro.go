package handler

// CEREBRO-PATCH(daemon-snapshot-compression): tiered snapshot compression
// (FIR-3074). When a snapshot exceeds snapshotMaxChars and the workspace has
// snapshot_compression = "on", this file compresses the older portion of the
// comment thread using a cheap Haiku model and returns a tiered snapshot:
//
//   - Issue header + description: verbatim
//   - Older comments (all except the most recent compressionRecentCount): Haiku summary
//   - Recent comments (last compressionRecentCount): verbatim
//
// Falls back to an empty string on Haiku failure; the caller then falls through
// to the size-guard (minimal prompt, agent fetches its own context).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	compressionRecentCount = 5
	compressionHaikuModel  = "claude-haiku-4-5"
	compressionTimeout     = 10 * time.Second
	compressionMaxTokens   = 1024
)

// compressionLine is a filtered comment line ready for snapshot rendering.
type compressionLine struct{ who, content string }

// compressIssueSnapshot builds a tiered snapshot for an oversized issue.
// It keeps the last compressionRecentCount comments verbatim and replaces
// older comments with a Haiku-generated summary. Returns "" if Haiku fails
// so the caller can fall through to the size-guard.
func compressIssueSnapshot(ctx context.Context, issue db.Issue, comments []db.Comment, triggerCommentID pgtype.UUID, selfAgentID string) string {
	// Filter comments using the same rules as renderIssueSnapshot.
	filtered := make([]compressionLine, 0, len(comments))
	for _, c := range comments {
		if triggerCommentID.Valid && c.ID == triggerCommentID {
			continue
		}
		if c.Type != "comment" {
			continue
		}
		content := strings.TrimSpace(c.Content)
		if content == "" {
			continue
		}
		who := "A user"
		switch c.AuthorType {
		case "agent":
			who = "An agent"
			if selfAgentID != "" && c.AuthorID.Valid && uuidToString(c.AuthorID) == selfAgentID {
				who = "You (earlier)"
			}
		case "system":
			who = "System"
		}
		filtered = append(filtered, compressionLine{who: who, content: content})
	}

	if len(filtered) <= compressionRecentCount {
		// Short enough to render normally — no compression needed.
		return renderIssueSnapshot(issue, comments, triggerCommentID, selfAgentID)
	}

	olderLines := filtered[:len(filtered)-compressionRecentCount]
	recentLines := filtered[len(filtered)-compressionRecentCount:]

	summary := summarizeCommentsWithHaiku(ctx, strings.TrimSpace(issue.Title), len(olderLines), buildTranscript(olderLines))

	var b strings.Builder
	b.WriteString("## Your issue (already fetched — do not re-fetch)\n\n")
	fmt.Fprintf(&b, "Issue #%d: %s\n", issue.Number, strings.TrimSpace(issue.Title))
	fmt.Fprintf(&b, "Status: %s · Priority: %s\n", issue.Status, issue.Priority)
	if desc := strings.TrimSpace(issue.Description.String); desc != "" {
		b.WriteString("\n")
		b.WriteString(desc)
		b.WriteString("\n")
	}

	if summary != "" {
		b.WriteString("\n## Earlier thread (AI-compressed — older comments summarised)\n\n")
		b.WriteString(summary)
		b.WriteString("\n")
	}

	b.WriteString("\n## Recent thread (oldest first; most recent last)\n\n")
	for _, l := range recentLines {
		fmt.Fprintf(&b, "%s:\n%s\n\n", l.who, l.content)
	}

	result := strings.TrimSpace(b.String())
	if result == "" {
		return ""
	}
	return result
}

// buildTranscript formats a slice of compressionLines into a plain text
// transcript suitable for passing to the Haiku summarisation prompt.
func buildTranscript(lines []compressionLine) string {
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "%s: %s\n\n", l.who, l.content)
	}
	return b.String()
}

// summarizeCommentsWithHaiku calls the Anthropic Messages API with the
// cheap Haiku model to produce a concise bullet-point summary of commentCount
// older comments represented as transcript text. Returns "" on any failure;
// the caller handles the fallback.
func summarizeCommentsWithHaiku(ctx context.Context, issueTitle string, commentCount int, transcript string) string {
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		slog.Warn("snapshot compression: ANTHROPIC_API_KEY not set, skipping Haiku call")
		return ""
	}

	systemMsg := "You summarise a work-in-progress issue comment thread for an AI agent that will continue the work. Preserve: current status, open decisions, PR and URL references, todo items, and the most recent blocker. Omit: pleasantries, resolved discussions, and superseded decisions. Output a concise bullet-point summary in the same language as the thread (Danish or English). Maximum 400 words."
	userMsg := fmt.Sprintf("Issue: %s\n\nThread to summarise (%d earlier comments):\n\n%s", issueTitle, commentCount, transcript)

	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type req struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system"`
		Messages  []msg  `json:"messages"`
	}
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type resp struct {
		Content []contentBlock `json:"content"`
		Error   *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	body, err := json.Marshal(req{
		Model:     compressionHaikuModel,
		MaxTokens: compressionMaxTokens,
		System:    systemMsg,
		Messages:  []msg{{Role: "user", Content: userMsg}},
	})
	if err != nil {
		slog.Warn("snapshot compression: marshal failed", "error", err)
		return ""
	}

	tctx, cancel := context.WithTimeout(ctx, compressionTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(tctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		slog.Warn("snapshot compression: build request failed", "error", err)
		return ""
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: compressionTimeout}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		slog.Warn("snapshot compression: haiku call failed", "error", err)
		return ""
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		slog.Warn("snapshot compression: read body failed", "error", err)
		return ""
	}

	if httpResp.StatusCode/100 != 2 {
		limit := min(len(raw), 200)
		slog.Warn("snapshot compression: haiku returned error status",
			"status", httpResp.StatusCode, "body", string(raw[:limit]))
		return ""
	}

	var parsed resp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		slog.Warn("snapshot compression: unmarshal failed", "error", err)
		return ""
	}
	if parsed.Error != nil {
		slog.Warn("snapshot compression: haiku api error", "message", parsed.Error.Message)
		return ""
	}
	for _, c := range parsed.Content {
		if c.Type == "text" && c.Text != "" {
			return strings.TrimSpace(c.Text)
		}
	}
	return ""
}
