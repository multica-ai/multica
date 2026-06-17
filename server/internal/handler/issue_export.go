package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const issueExportContentType = "text/markdown; charset=utf-8"

// ExportIssue streams a Markdown snapshot of an issue's metadata, timeline, and
// agent run history for external review.
func (h *Handler) ExportIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	ctx := r.Context()
	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       timelineHardCap,
	})
	if err != nil {
		slog.Warn("ExportIssue ListCommentsForIssue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	activities, err := h.Queries.ListActivitiesForIssue(ctx, db.ListActivitiesForIssueParams{
		IssueID: issue.ID,
		Limit:   timelineHardCap,
	})
	if err != nil {
		slog.Warn("ExportIssue ListActivitiesForIssue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list activities")
		return
	}
	timeline := h.mergeTimeline(r, comments, activities, true)

	tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID)
	if err != nil {
		slog.Warn("ExportIssue ListTasksByIssue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	sort.Slice(tasks, func(i, j int) bool {
		return timestampSortKey(tasks[i].CreatedAt) < timestampSortKey(tasks[j].CreatedAt)
	})

	messagesByTask := make(map[string][]db.TaskMessage, len(tasks))
	for _, task := range tasks {
		taskID := uuidToString(task.ID)
		messages, err := h.Queries.ListTaskMessages(ctx, task.ID)
		if err != nil {
			slog.Warn("ExportIssue ListTaskMessages failed", append(logger.RequestAttrs(r), "error", err, "task_id", taskID, "issue_id", uuidToString(issue.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to list task messages")
			return
		}
		messagesByTask[taskID] = messages
	}

	issuePrefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	identifier := issuePrefix + "-" + strconv.Itoa(int(issue.Number))
	body := renderIssueExportMarkdown(issue, identifier, timeline, tasks, messagesByTask)
	filename := sanitizeDownloadFilename("issue-" + identifier + "-export.md")

	w.Header().Set("Content-Type", issueExportContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(body))
}

func renderIssueExportMarkdown(issue db.Issue, identifier string, timeline []TimelineEntry, tasks []db.AgentTaskQueue, messagesByTask map[string][]db.TaskMessage) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(escapeMarkdownHeading(identifier + ": " + issue.Title))
	b.WriteString("\n\n")

	b.WriteString("## Metadata\n\n")
	writeMarkdownKV(&b, "ID", uuidToString(issue.ID))
	writeMarkdownKV(&b, "Workspace ID", uuidToString(issue.WorkspaceID))
	writeMarkdownKV(&b, "Identifier", identifier)
	writeMarkdownKV(&b, "Status", issue.Status)
	writeMarkdownKV(&b, "Priority", issue.Priority)
	writeMarkdownKV(&b, "Assignee", actorRef(issue.AssigneeType, issue.AssigneeID))
	writeMarkdownKV(&b, "Creator", actorRef(pgtype.Text{String: issue.CreatorType, Valid: true}, issue.CreatorID))
	writeMarkdownKV(&b, "Parent Issue ID", optionalUUID(issue.ParentIssueID))
	writeMarkdownKV(&b, "Project ID", optionalUUID(issue.ProjectID))
	writeMarkdownKV(&b, "Start Date", optionalDate(issue.StartDate))
	writeMarkdownKV(&b, "Due Date", optionalDate(issue.DueDate))
	writeMarkdownKV(&b, "Created", timestampToString(issue.CreatedAt))
	writeMarkdownKV(&b, "Updated", timestampToString(issue.UpdatedAt))
	b.WriteString("\n")

	b.WriteString("## Description\n\n")
	if strings.TrimSpace(issue.Description.String) == "" {
		b.WriteString("_No description._\n\n")
	} else {
		b.WriteString(issue.Description.String)
		b.WriteString("\n\n")
	}

	b.WriteString("## Issue Metadata\n\n")
	writeJSONFence(&b, parseIssueMetadata(issue.Metadata))
	b.WriteString("\n")

	b.WriteString("## Timeline\n\n")
	if len(timeline) == 0 {
		b.WriteString("_No timeline entries._\n\n")
	} else {
		for _, entry := range timeline {
			writeTimelineEntry(&b, entry)
		}
	}

	b.WriteString("## Agent Runs\n\n")
	if len(tasks) == 0 {
		b.WriteString("_No agent runs._\n")
	} else {
		workspaceID := uuidToString(issue.WorkspaceID)
		for _, task := range tasks {
			writeTaskRun(&b, task, workspaceID, messagesByTask[uuidToString(task.ID)])
		}
	}

	return b.String()
}

func writeTimelineEntry(b *strings.Builder, entry TimelineEntry) {
	actor := entry.ActorType
	if entry.ActorID != "" {
		actor += ":" + entry.ActorID
	}
	b.WriteString("### ")
	b.WriteString(entry.CreatedAt)
	b.WriteString(" — ")
	b.WriteString(entry.Type)
	if actor != "" {
		b.WriteString(" — ")
		b.WriteString(actor)
	}
	b.WriteString("\n\n")

	switch entry.Type {
	case "comment":
		if entry.CommentType != nil && *entry.CommentType != "" {
			writeMarkdownKV(b, "Comment Type", *entry.CommentType)
		}
		if entry.ParentID != nil && *entry.ParentID != "" {
			writeMarkdownKV(b, "Parent Comment ID", *entry.ParentID)
		}
		if entry.SourceTaskID != nil && *entry.SourceTaskID != "" {
			writeMarkdownKV(b, "Source Task ID", *entry.SourceTaskID)
		}
		if entry.ResolvedAt != nil && *entry.ResolvedAt != "" {
			writeMarkdownKV(b, "Resolved At", *entry.ResolvedAt)
		}
		if entry.Content != nil && strings.TrimSpace(*entry.Content) != "" {
			b.WriteString("\n")
			b.WriteString(*entry.Content)
			b.WriteString("\n")
		}
		if len(entry.Attachments) > 0 {
			b.WriteString("\nAttachments:\n")
			for _, att := range entry.Attachments {
				b.WriteString("- ")
				b.WriteString(att.Filename)
				b.WriteString(" (`")
				b.WriteString(att.ID)
				b.WriteString("`, ")
				b.WriteString(att.ContentType)
				b.WriteString(", ")
				b.WriteString(fmt.Sprintf("%d bytes", att.SizeBytes))
				b.WriteString(")\n")
			}
		}
	case "activity":
		if entry.Action != nil {
			writeMarkdownKV(b, "Action", *entry.Action)
		}
		if len(entry.Details) > 0 {
			b.WriteString("\nDetails:\n\n")
			writeJSONFence(b, json.RawMessage(entry.Details))
		}
	}
	b.WriteString("\n")
}

func writeTaskRun(b *strings.Builder, task db.AgentTaskQueue, workspaceID string, messages []db.TaskMessage) {
	taskID := uuidToString(task.ID)
	b.WriteString("### Run ")
	b.WriteString(taskID)
	b.WriteString("\n\n")
	writeMarkdownKV(b, "Agent ID", uuidToString(task.AgentID))
	writeMarkdownKV(b, "Runtime ID", uuidToString(task.RuntimeID))
	writeMarkdownKV(b, "Status", task.Status)
	writeMarkdownKV(b, "Attempt", fmt.Sprintf("%d/%d", task.Attempt, task.MaxAttempts))
	writeMarkdownKV(b, "Created", timestampToString(task.CreatedAt))
	writeMarkdownKV(b, "Dispatched", optionalTimestamp(task.DispatchedAt))
	writeMarkdownKV(b, "Started", optionalTimestamp(task.StartedAt))
	writeMarkdownKV(b, "Completed", optionalTimestamp(task.CompletedAt))
	writeMarkdownKV(b, "Failure Reason", optionalText(task.FailureReason))
	writeMarkdownKV(b, "Error", optionalText(task.Error))
	writeMarkdownKV(b, "Trigger Comment ID", optionalUUID(task.TriggerCommentID))
	writeMarkdownKV(b, "Trigger Summary", optionalText(task.TriggerSummary))
	if task.WorkDir.Valid {
		writeMarkdownKV(b, "Workdir", relativeWorkDir(task.WorkDir.String, workspaceID, taskID))
	}
	if len(task.Result) > 0 {
		b.WriteString("\nResult:\n\n")
		writeJSONFence(b, json.RawMessage(task.Result))
	}
	if len(messages) == 0 {
		b.WriteString("\n_No task messages._\n\n")
		return
	}
	b.WriteString("\nMessages:\n\n")
	for _, msg := range messages {
		writeTaskMessage(b, msg)
	}
}

func writeTaskMessage(b *strings.Builder, msg db.TaskMessage) {
	b.WriteString("#### Seq ")
	b.WriteString(strconv.Itoa(int(msg.Seq)))
	b.WriteString(" — ")
	b.WriteString(msg.Type)
	if msg.Tool.Valid && msg.Tool.String != "" {
		b.WriteString(" — ")
		b.WriteString(msg.Tool.String)
	}
	b.WriteString("\n\n")
	writeMarkdownKV(b, "Created", timestampToString(msg.CreatedAt))
	if msg.Content.Valid && strings.TrimSpace(msg.Content.String) != "" {
		b.WriteString("\n")
		b.WriteString(msg.Content.String)
		b.WriteString("\n")
	}
	if len(msg.Input) > 0 {
		b.WriteString("\nInput:\n\n")
		writeJSONFence(b, json.RawMessage(msg.Input))
	}
	if msg.Output.Valid && strings.TrimSpace(msg.Output.String) != "" {
		b.WriteString("\nOutput:\n\n")
		writeFence(b, "", msg.Output.String)
	}
	b.WriteString("\n")
}

func writeMarkdownKV(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		value = "—"
	}
	b.WriteString("- ")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

func actorRef(actorType pgtype.Text, actorID pgtype.UUID) string {
	if !actorType.Valid || actorType.String == "" || !actorID.Valid {
		return "—"
	}
	return actorType.String + ":" + uuidToString(actorID)
}

func optionalUUID(id pgtype.UUID) string {
	if !id.Valid {
		return "—"
	}
	return uuidToString(id)
}

func optionalText(t pgtype.Text) string {
	if !t.Valid || strings.TrimSpace(t.String) == "" {
		return "—"
	}
	return t.String
}

func optionalTimestamp(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return "—"
	}
	return timestampToString(ts)
}

func optionalDate(d pgtype.Date) string {
	if !d.Valid {
		return "—"
	}
	return d.Time.Format("2006-01-02")
}

func timestampSortKey(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339Nano)
}

func writeJSONFence(b *strings.Builder, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		writeFence(b, "json", fmt.Sprintf("%v", v))
		return
	}
	writeFence(b, "json", string(data))
}

func writeFence(b *strings.Builder, lang, body string) {
	fence := markdownFence(body)
	b.WriteString(fence)
	if lang != "" {
		b.WriteString(lang)
	}
	b.WriteString("\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fence)
	b.WriteString("\n")
}

func markdownFence(body string) string {
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return fence
}

func escapeMarkdownHeading(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

func sanitizeDownloadFilename(name string) string {
	var b bytes.Buffer
	b.Grow(len(name))
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '\\' || r == '\x00' || r == '/' {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "issue-export.md"
	}
	return out
}
