package qoderruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type event struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content []struct {
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
func (b *Bridge) events(ctx context.Context) error {
	r := b.state.Run
	q := url.Values{"limit": {"100"}, "order": {"asc"}}
	if r.Page != "" {
		q.Set("page", r.Page)
	} else if r.Cursor != "" {
		q.Set("after_id", r.Cursor)
	}
	var page struct {
		Data     []event `json:"data"`
		HasMore  bool    `json:"has_more"`
		NextPage string  `json:"next_page"`
	}
	if err := b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.SessionID)+"/events?"+q.Encode(), nil, &page); err != nil {
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
		case "agent.message":
			msg["type"] = "text"
			msg["content"] = e.text()
			r.Output = e.text()
		case "agent.tool_use", "agent.mcp_tool_use":
			msg["type"] = "tool_use"
			msg["tool"] = e.Name
			msg["input"] = e.Input
		case "agent.tool_result", "agent.mcp_tool_result":
			msg["type"] = "tool_result"
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
			if err := b.taskCall(ctx, "/messages", map[string]any{"messages": []map[string]any{msg}}, nil); err != nil {
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
			return b.deliver(ctx)
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
