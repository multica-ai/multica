package qoderruntime

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
	Type      string `json:"type"`
	Name      string `json:"name"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Input               map[string]any `json:"input"`
	EvaluatedPermission string         `json:"evaluated_permission"`
	McpServerName       string         `json:"mcp_server_name"`
	StopReason          struct {
		Type     string   `json:"type"`
		EventIDs []string `json:"event_ids"`
	} `json:"stop_reason"`
	Error struct {
		Message     string `json:"message"`
		RetryStatus struct {
			Type string `json:"type"`
		} `json:"retry_status"`
	} `json:"error"`
	// ModelUsage is set on span.model_request_end; TotalCredits on
	// session.usage, where it is the session's cumulative charge.
	ModelUsage struct {
		Credits *float64 `json:"credits"`
	} `json:"model_usage"`
	TotalCredits *float64 `json:"total_credits"`
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

// eventPageLimits are tried in order. Qoder returns image blocks in full in
// user.message events, so a page of them can exceed the response limit even
// though each message fits; a smaller page is read instead.
var eventPageLimits = []string{"100", "10", "1"}

type eventPage struct {
	Data     []event `json:"data"`
	HasMore  bool    `json:"has_more"`
	NextPage string  `json:"next_page"`
}

func (b *Bridge) events(ctx context.Context, r *run) error {
	var q url.Values
	var page eventPage
	var err error
	for i, limit := range eventPageLimits {
		q = url.Values{"limit": {limit}, "order": {"asc"}}
		// An opaque page cursor may carry its own page size, so a smaller
		// retry resumes after the last processed event instead.
		if r.Page != "" && (i == 0 || r.Cursor == "") {
			q.Set("page", r.Page)
		} else if r.Cursor != "" {
			q.Set("after_id", r.Cursor)
		}
		page = eventPage{}
		err = b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.SessionID)+"/events?"+q.Encode(), nil, &page)
		if !errors.Is(err, errResponseTooLarge) {
			break
		}
	}
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return b.reject(ctx, r, "Qoder session no longer exists")
		}
		return err
	}
	for _, e := range page.Data {
		if e.ID == "" {
			return fmt.Errorf("Qoder event has no ID")
		}
		if e.ID == r.Cursor {
			continue
		}
		// A new session can emit an initial idle before the user message. Ignore it.
		if e.Type == "user.message" && e.text() == r.Prompt {
			r.Started = true
			r.Phase = "running"
		}
		if !r.Started {
			r.Cursor = e.ID
			r.Page = ""
			continue
		}
		msg := map[string]any{}
		switch e.Type {
		case "span.model_request_end":
			if e.ModelUsage.Credits != nil {
				r.addSpanCredits(*e.ModelUsage.Credits)
			}
		case "session.usage":
			if e.TotalCredits != nil {
				r.setSessionCredits(*e.TotalCredits)
			}
		case "agent.message":
			msg["type"] = "text"
			msg["content"] = e.text()
			r.Output = e.text()
		case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
			msg["type"] = "tool_use"
			msg["call_id"] = e.ID
			msg["tool"] = e.Name
			msg["input"] = e.Input
			r.trackAction(e)
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
			if e.StopReason.Type == "requires_action" && len(e.StopReason.EventIDs) > 0 && r.Failure == "" {
				r.Phase = "awaiting_confirmation"
				r.Awaiting = append([]string(nil), e.StopReason.EventIDs...)
				break
			}
			r.Phase = "final"
			if e.StopReason.Type == "end_turn" && r.Failure == "" {
				r.Outcome = "completed"
			} else {
				r.Outcome = "failed"
				if r.Failure == "" {
					r.Failure = "Qoder stopped: " + e.StopReason.Type
				}
			}
		case "session.status_terminated", "session.deleted":
			r.Phase = "final"
			r.Outcome = "failed"
			r.Failure = "Qoder session terminated"
		}
		if len(msg) > 0 {
			msg["seq"] = r.Seq + 1
			msg["idempotency_key"] = "qoder:" + e.ID
			if err := b.taskCall(ctx, r, "/messages", map[string]any{"messages": []map[string]any{msg}}, nil); err != nil {
				return err
			}
			r.Seq++
		}
		r.Cursor = e.ID
		r.Page = ""
		if err := b.persist(); err != nil {
			return err
		}
		if r.Phase == "final" {
			return b.deliver(ctx, r)
		}
		if r.Phase == "awaiting_confirmation" {
			return b.resolveActions(ctx, r)
		}
	}
	r.Page = ""
	if page.HasMore {
		if page.NextPage == "" || page.NextPage == q.Get("page") {
			return fmt.Errorf("Qoder pagination cursor did not advance")
		}
		r.Page = page.NextPage
	}
	if err := b.persist(); err != nil {
		return err
	}
	if r.Phase == "sending" {
		return fmt.Errorf("submission not yet confirmed; inspect Qoder session %s before any manual resubmission", r.SessionID)
	}
	return nil
}

// latestEventID returns the newest event of a session, or "" when it has none.
// A continued session starts reading after it, so the new turn never re-reads
// earlier turns (whose images alone may exceed the response limit) and never
// mistakes an earlier idle event for its own completion.
func (b *Bridge) latestEventID(ctx context.Context, sessionID string) (string, error) {
	q := url.Values{"limit": {"1"}, "order": {"desc"}}
	var page eventPage
	if err := b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID)+"/events?"+q.Encode(), nil, &page); err != nil {
		return "", err
	}
	if len(page.Data) == 0 {
		return "", nil
	}
	if page.Data[0].ID == "" {
		return "", fmt.Errorf("Qoder event has no ID")
	}
	return page.Data[0].ID, nil
}
