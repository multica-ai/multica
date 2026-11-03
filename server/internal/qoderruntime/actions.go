package qoderruntime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// pendingAction is a tool call that can pause a Qoder session with
// requires_action: an `always_ask` tool, or a client-side custom tool.
type pendingAction struct {
	Name          string         `json:"name"`
	McpServerName string         `json:"mcp_server_name,omitempty"`
	Input         map[string]any `json:"input,omitempty"`
	Custom        bool           `json:"custom,omitempty"`
}

func (r *run) trackAction(e event) {
	custom := e.Type == "agent.custom_tool_use"
	if !custom && e.EvaluatedPermission != "ask" {
		return
	}
	if r.Actions == nil {
		r.Actions = map[string]pendingAction{}
	}
	r.Actions[e.ID] = pendingAction{Name: e.Name, McpServerName: e.McpServerName, Input: e.Input, Custom: custom}
}

type toolConfirmation struct {
	CallID      string `json:"call_id"`
	Status      string `json:"status"`
	DenyMessage string `json:"deny_message"`
}

// resolveActions drives a requires_action pause. Custom tools cannot run
// inside Multica, so they are answered with an error at once; every other
// call is registered with Multica and answered once a member decides it.
func (b *Bridge) resolveActions(ctx context.Context, r *run) error {
	if err := b.answerCustomTools(ctx, r); err != nil || r.Phase == "final" {
		return err
	}
	if len(r.Awaiting) > 0 {
		decisions, err := b.registerConfirmations(ctx, r)
		if err != nil || r.Phase == "final" || decisions == nil {
			return err
		}
		if err := b.sendConfirmations(ctx, r, decisions); err != nil || r.Phase == "final" || len(r.Awaiting) > 0 {
			return err
		}
	}
	r.Phase = "running"
	if err := b.persist(); err != nil {
		return err
	}
	return b.events(ctx, r)
}

func (b *Bridge) answerCustomTools(ctx context.Context, r *run) error {
	var remaining []string
	for _, id := range r.Awaiting {
		action, ok := r.Actions[id]
		if !ok || !action.Custom {
			remaining = append(remaining, id)
			continue
		}
		text := fmt.Sprintf("Custom tool %q cannot be executed by Multica. Continue without it.", action.Name)
		err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/events", map[string]any{"events": []map[string]any{{
			"type":               "user.custom_tool_result",
			"custom_tool_use_id": id,
			"content":            []map[string]any{{"type": "text", "text": text}},
			"is_error":           true,
		}}}, nil)
		if err != nil {
			if definitelyRejected(err) {
				return b.reject(ctx, r, "Qoder rejected the custom tool result: "+err.Error())
			}
			return err
		}
		if err := b.postMessage(ctx, r, "custom:"+id, map[string]any{"type": "error", "content": text}); err != nil {
			return err
		}
		delete(r.Actions, id)
	}
	r.Awaiting = remaining
	return b.persist()
}

// registerConfirmations returns the decisions for every awaited call, or nil
// while any is still pending.
func (b *Bridge) registerConfirmations(ctx context.Context, r *run) (map[string]toolConfirmation, error) {
	calls := make([]map[string]any, 0, len(r.Awaiting))
	for _, id := range r.Awaiting {
		action, ok := r.Actions[id]
		if !ok {
			action = pendingAction{Name: "unknown tool"}
		}
		call := map[string]any{"call_id": id, "tool_name": action.Name, "input": action.Input}
		if action.McpServerName != "" {
			call["mcp_server_name"] = action.McpServerName
		}
		calls = append(calls, call)
	}
	var response struct {
		Confirmations []toolConfirmation `json:"confirmations"`
	}
	if err := b.taskCall(ctx, r, "/tool-confirmations", map[string]any{"confirmations": calls}, &response); err != nil {
		if definitelyRejected(err) {
			b.queueCleanup(r.SessionID)
			if err := b.persist(); err != nil {
				return nil, err
			}
			b.cleanup(ctx)
			return nil, b.reject(ctx, r, "Qoder requested tool approval ("+strings.Join(actionNames(r), ", ")+"), which Multica supports only for issue runs: "+err.Error())
		}
		return nil, err
	}
	decisions := make(map[string]toolConfirmation, len(response.Confirmations))
	for _, c := range response.Confirmations {
		decisions[c.CallID] = c
	}
	for _, id := range r.Awaiting {
		if c, ok := decisions[id]; !ok || c.Status == "pending" {
			return nil, nil
		}
	}
	return decisions, nil
}

func (b *Bridge) sendConfirmations(ctx context.Context, r *run, decisions map[string]toolConfirmation) error {
	events := make([]map[string]any, 0, len(r.Awaiting))
	for _, id := range r.Awaiting {
		c := decisions[id]
		reply := map[string]any{"type": "user.tool_confirmation", "tool_use_id": id, "result": "allow"}
		if c.Status != "allowed" {
			reply["result"] = "deny"
			if c.DenyMessage != "" {
				reply["deny_message"] = c.DenyMessage
			}
		}
		events = append(events, reply)
	}
	err := b.qoder.call(ctx, http.MethodPost, "/sessions/"+url.PathEscape(r.SessionID)+"/events", map[string]any{"events": events}, nil)
	if err != nil {
		if rateLimited(err) {
			return nil
		}
		if definitelyRejected(err) {
			return b.reject(ctx, r, "Qoder rejected the tool confirmation: "+err.Error())
		}
		return err
	}
	for _, id := range r.Awaiting {
		delete(r.Actions, id)
	}
	r.Awaiting = nil
	return b.persist()
}

func actionNames(r *run) []string {
	names := make([]string, 0, len(r.Awaiting))
	for _, id := range r.Awaiting {
		if a, ok := r.Actions[id]; ok {
			names = append(names, a.Name)
		}
	}
	return names
}

// postMessage appends one idempotent timeline message to the Multica run.
func (b *Bridge) postMessage(ctx context.Context, r *run, key string, msg map[string]any) error {
	msg["seq"] = r.Seq + 1
	msg["idempotency_key"] = "qoder:" + key
	if err := b.taskCall(ctx, r, "/messages", map[string]any{"messages": []map[string]any{msg}}, nil); err != nil {
		return err
	}
	r.Seq++
	return nil
}
