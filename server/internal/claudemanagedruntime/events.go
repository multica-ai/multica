package claudemanagedruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type event struct {
	ToolUseID string `json:"tool_use_id"`
	ID        string `json:"id"`
	// ProcessedAt is null for a sent event still queued.
	ProcessedAt string `json:"processed_at"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Content     []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Input      map[string]any `json:"input"`
	StopReason struct {
		Type string `json:"type"`
	} `json:"stop_reason"`
	Error struct {
		Message     string `json:"message"`
		RetryStatus struct {
			Type string `json:"type"`
		} `json:"retry_status"`
	} `json:"error"`
}

func (e event) text() string {
	var parts []string
	for _, c := range e.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
func (b *Bridge) events(ctx context.Context, r *run) error {
	q := url.Values{"limit": {"100"}, "order": {"asc"}}
	// Without a page cursor, list again from the last processed event's
	// processed_at (created_at filters compare it, inclusively) and skip every
	// event up to and including Cursor: events can share a timestamp.
	// A page cursor is sent with the filters of the request that returned it.
	skipping := r.Page == "" && r.Cursor != ""
	if r.Page != "" {
		q.Set("page", r.Page)
		if r.PageFrom != "" {
			q.Set("created_at[gte]", r.PageFrom)
		}
	} else if r.CursorAt != "" {
		q.Set("created_at[gte]", r.CursorAt)
	}
	var page struct {
		Data     []event `json:"data"`
		NextPage string  `json:"next_page"`
	}
	var fresh []event
	for {
		page.Data, page.NextPage = nil, ""
		if err := b.anthropic.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.SessionID)+"/events?"+q.Encode(), nil, &page); err != nil {
			var ae *apiError
			if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
				return b.reject(ctx, r, "Claude session no longer exists")
			}
			return err
		}
		fresh = page.Data
		if skipping {
			fresh = nil
			for i, e := range page.Data {
				if e.ID == r.Cursor {
					fresh, skipping = page.Data[i+1:], false
					break
				}
			}
		}
		if !skipping || page.NextPage == "" {
			break
		}
		if page.NextPage == q.Get("page") {
			return fmt.Errorf("Claude pagination cursor did not advance")
		}
		q.Set("page", page.NextPage)
	}
	if skipping {
		return fmt.Errorf("Claude event %s is missing from session %s history", r.Cursor, r.SessionID)
	}
	for _, e := range fresh {
		if e.ID == "" {
			return fmt.Errorf("Claude event has no ID")
		}
		if e.ID == r.Cursor {
			continue
		}
		// A new session starts at creation. A continued session's history holds
		// earlier runs, so its run starts at the user message carrying the prompt.
		if !r.Started && e.Type == "user.message" && e.text() == r.Prompt {
			r.Started = true
			r.Phase = "running"
		}
		if !r.Started {
			r.Cursor, r.CursorAt = e.ID, e.ProcessedAt
			r.Page, r.PageFrom = "", ""
			continue
		}
		msg := map[string]any{}
		switch e.Type {
		case "agent.message":
			msg["type"] = "text"
			msg["content"] = e.text()
			r.Output = e.text()
		case "agent.tool_use", "agent.mcp_tool_use":
			msg["type"] = "tool_use"
			msg["call_id"] = e.ID
			msg["tool"] = e.Name
			msg["input"] = e.Input
		case "agent.tool_result", "agent.mcp_tool_result":
			msg["type"] = "tool_result"
			if e.ToolUseID != "" {
				msg["call_id"] = e.ToolUseID
			}
			msg["output"] = e.text()
		case "session.error":
			msg["type"] = "error"
			msg["content"] = e.Error.Message
			if e.Error.RetryStatus.Type != "retrying" {
				r.Failure = e.Error.Message
			}
		case "session.status_idle":
			r.Phase = "final"
			if e.StopReason.Type == "end_turn" && r.Failure == "" {
				r.Outcome = "completed"
			} else {
				r.Outcome = "failed"
				if r.Failure == "" {
					r.Failure = "Claude stopped: " + e.StopReason.Type
				}
			}
		case "session.status_terminated", "session.deleted":
			r.Phase = "final"
			r.Outcome = "failed"
			r.Failure = "Claude session terminated"
		}
		if len(msg) > 0 {
			msg["seq"] = r.Seq + 1
			msg["idempotency_key"] = "claude:" + e.ID
			if err := b.taskCall(ctx, r, "/messages", map[string]any{"messages": []map[string]any{msg}}, nil); err != nil {
				return err
			}
			r.Seq++
		}
		r.Cursor, r.CursorAt = e.ID, e.ProcessedAt
		r.Page, r.PageFrom = "", ""
		if err := b.persist(); err != nil {
			return err
		}
		if r.Phase == "final" {
			return b.deliver(ctx, r)
		}
	}
	r.Page, r.PageFrom = "", ""
	if page.NextPage != "" {
		if page.NextPage == q.Get("page") {
			return fmt.Errorf("Claude pagination cursor did not advance")
		}
		r.Page, r.PageFrom = page.NextPage, q.Get("created_at[gte]")
	}
	if err := b.persist(); err != nil {
		return err
	}
	if r.Phase == "sending" {
		return fmt.Errorf("submission not yet confirmed; inspect Claude session %s before any manual resubmission", r.SessionID)
	}
	return nil
}
