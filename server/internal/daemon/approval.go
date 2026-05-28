package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/trace"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// BuildApprovalCallback returns an agent.ApprovalCallback based on the
// approval policy. The callback is injected into ExecOptions.OnApproval.
//
//   - auto:   returns nil (adapter auto-approves, no interaction created)
//   - deny:   returns a callback that immediately denies
//   - prompt: returns a callback that reports to the server and polls for response
func BuildApprovalCallback(policy, taskID, providerName string, client *Client) agent.ApprovalCallback {
	switch policy {
	case protocol.ApprovalPolicyDeny:
		return func(_ context.Context, _ agent.ApprovalRequest) (string, bool, error) {
			return agent.EncodeApprovalChoice("deny", agent.CachedApprovalResponseMessage), false, nil
		}
	case protocol.ApprovalPolicyPrompt:
		var allowSimilar sync.Map
		return func(ctx context.Context, req agent.ApprovalRequest) (string, bool, error) {
			key := approvalSimilarKey(req)
			if _, ok := allowSimilar.Load(key); ok {
				return agent.EncodeApprovalChoice("accept_similar", agent.CachedApprovalResponseMessage), true, nil
			}
			chosen, approved, err := promptViaServer(ctx, taskID, providerName, client, req)
			if err == nil && approved && chosen == "accept_similar" {
				allowSimilar.Store(key, struct{}{})
			}
			return chosen, approved, err
		}
	default: // auto
		return nil
	}
}

func BuildPlanAwareApprovalCallback(policy, taskID, providerName string, client *Client) agent.ApprovalCallback {
	base := BuildApprovalCallback(policy, taskID, providerName, client)
	return func(ctx context.Context, req agent.ApprovalRequest) (string, bool, error) {
		if req.Type == protocol.InteractionPlanApproval {
			chosen, approved, err := promptViaServer(ctx, taskID, providerName, client, req)
			// When plan is approved, report the plan content as a task message
			// so it's visible in the issue timeline before execution begins.
			if approved && req.Detail != "" {
				reportPlanAsMessage(ctx, taskID, client, req.Detail)
			}
			return chosen, approved, err
		}
		if base == nil {
			return "allow", true, nil
		}
		return base(ctx, req)
	}
}

// reportPlanAsMessage sends the approved plan content as a task message so it
// appears in the issue timeline. This lets reviewers see what plan was approved
// without having to inspect the execution output.
func reportPlanAsMessage(ctx context.Context, taskID string, client *Client, planContent string) {
	_ = client.ReportTaskMessages(ctx, taskID, []TaskMessageData{{
		Seq:     0,
		Type:    "text",
		Content: "**Approved plan:**\n\n" + planContent,
	}})
}

func approvalSimilarKey(req agent.ApprovalRequest) string {
	t := strings.TrimSpace(req.Type)
	if t == "" {
		t = "approval"
	}
	title := strings.TrimSpace(req.Title)
	signature := approvalTargetSignature(req)
	if signature == "" {
		signature = normalizedApprovalText(req.Detail)
	}
	if signature == "" {
		signature = normalizedApprovalText(title)
	}
	if signature == "" {
		return t
	}
	return t + "|" + signature
}

func approvalTargetSignature(req agent.ApprovalRequest) string {
	switch strings.TrimSpace(req.Type) {
	case "command_approval":
		if command := normalizedApprovalText(req.Detail); command != "" {
			return "command:" + command
		}
	case "file_change_approval":
		if path := approvalPathFromDetail(req.Detail); path != "" {
			return "path:" + path
		}
		if title := strings.TrimPrefix(strings.TrimSpace(req.Title), "Write: "); title != strings.TrimSpace(req.Title) {
			return "path:" + filepath.Clean(title)
		}
	case "permission_request":
		if detail := strings.TrimSpace(req.Detail); strings.HasPrefix(detail, "{") {
			var payload map[string]any
			if json.Unmarshal([]byte(detail), &payload) == nil {
				if command, _ := payload["command"].(string); normalizedApprovalText(command) != "" {
					return "command:" + normalizedApprovalText(command)
				}
				for _, key := range []string{"path", "file_path", "notebook_path", "blocked_path"} {
					if value, _ := payload[key].(string); strings.TrimSpace(value) != "" {
						return "path:" + filepath.Clean(value)
					}
				}
			}
		}
	}
	return ""
}

func approvalPathFromDetail(detail string) string {
	if strings.TrimSpace(detail) == "" {
		return ""
	}
	var payload map[string]any
	if json.Unmarshal([]byte(detail), &payload) != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "notebook_path"} {
		if value, _ := payload[key].(string); strings.TrimSpace(value) != "" {
			return filepath.Clean(value)
		}
	}
	return ""
}

func normalizedApprovalText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

// promptViaServer reports an interaction to the server, then polls until
// the user responds or the context is cancelled / times out.
func promptViaServer(ctx context.Context, taskID, providerName string, client *Client, req agent.ApprovalRequest) (string, bool, error) {
	options := req.Options
	if len(options) == 0 {
		options = []protocol.InteractionOption{
			{ID: "allow", Label: "Allow"},
			{ID: "deny", Label: "Deny"},
		}
	}
	defaultOption := req.DefaultOption
	if defaultOption == "" {
		defaultOption = "deny"
	}
	body := map[string]any{
		"type":           req.Type,
		"title":          req.Title,
		"detail":         req.Detail,
		"provider":       providerName,
		"options":        options,
		"default_option": defaultOption,
		"expires_in":     -1,
	}

	interactionID, err := client.ReportInteraction(ctx, taskID, body)
	if err != nil {
		return "deny", false, fmt.Errorf("report interaction: %w", err)
	}

	// Poll every 2 seconds until resolved.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if req.Type == protocol.InteractionPlanApproval {
				return "timeout", false, ctx.Err()
			}
			if req.Type == protocol.InteractionCommandApproval {
				return protocol.InteractionStatusTimedOut, false, ctx.Err()
			}
			return "deny", false, ctx.Err()
		case <-ticker.C:
			resp, err := client.GetInteraction(ctx, taskID, interactionID)
			if err != nil {
				// Server unreachable or interaction gone — treat as timed out.
				return "deny", false, fmt.Errorf("poll interaction: %w", err)
			}
			status, _ := resp["status"].(string)
			switch status {
			case protocol.InteractionStatusApproved:
				chosen, _ := resp["chosen_option"].(string)
				if chosen == "" {
					chosen = "allow"
				}
				message, _ := resp["response_message"].(string)
				chosen = agent.EncodeApprovalChoice(chosen, message)
				return chosen, true, nil
			case protocol.InteractionStatusDenied:
				chosen, _ := resp["chosen_option"].(string)
				if chosen == "" {
					chosen = "deny"
				}
				message, _ := resp["response_message"].(string)
				chosen = agent.EncodeApprovalChoice(chosen, message)
				return chosen, false, nil
			case protocol.InteractionStatusTimedOut, protocol.InteractionStatusCancelled:
				if req.Type == protocol.InteractionPlanApproval {
					return status, false, fmt.Errorf("plan approval %s", status)
				}
				if req.Type == protocol.InteractionCommandApproval {
					return status, false, nil
				}
				return "deny", false, nil
				// pending — keep polling
			}
		}
	}
}

// WithApprovalTrace wraps an ApprovalCallback to write approval request/response
// events to the given trace store. The original callback behaviour is preserved.
//
// If store is nil or cb is nil, it returns cb unchanged (no-op).
// This is the integration point for writing approval events into the agent trace
// timeline alongside other channels (raw_stdout, provider_event, etc.).
func WithApprovalTrace(cb agent.ApprovalCallback, store trace.TraceStore, taskID, runID, providerName string) agent.ApprovalCallback {
	return WithApprovalTraceFilter(cb, store, taskID, runID, providerName, nil)
}

func WithApprovalTraceFilter(cb agent.ApprovalCallback, store trace.TraceStore, taskID, runID, providerName string, shouldTrace func(agent.ApprovalRequest) bool) agent.ApprovalCallback {
	if store == nil || cb == nil {
		return cb
	}
	return func(ctx context.Context, req agent.ApprovalRequest) (string, bool, error) {
		chosen, approved, err := cb(ctx, req)
		chosenForTrace, responseMessage := agent.SplitApprovalChoice(chosen)
		traceThis := shouldTrace == nil || shouldTrace(req)
		if responseMessage == agent.CachedApprovalResponseMessage {
			traceThis = false
		}

		if traceThis {
			// Write request and response only for approvals that were actually
			// surfaced to the user. Cached accept-similar decisions are hidden.
			_, _ = store.Append(ctx, trace.TraceLine{
				TaskID:     taskID,
				RunID:      runID,
				Provider:   providerName,
				Channel:    trace.ChannelApprovalRequest,
				Content:    req.Title,
				RawPayload: req.Detail,
			})
			respContent := fmt.Sprintf("%s (approved=%v)", chosenForTrace, approved)
			if responseMessage != "" && responseMessage != agent.CachedApprovalResponseMessage {
				respContent += ": " + responseMessage
			}
			if err != nil {
				respContent = fmt.Sprintf("error: %v", err)
			}
			_, _ = store.Append(ctx, trace.TraceLine{
				TaskID:   taskID,
				RunID:    runID,
				Provider: providerName,
				Channel:  trace.ChannelApprovalResponse,
				Content:  respContent,
			})
		}

		return chosen, approved, err
	}
}
