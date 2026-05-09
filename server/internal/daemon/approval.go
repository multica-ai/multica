package daemon

import (
	"context"
	"fmt"
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
			return "deny", false, nil
		}
	case protocol.ApprovalPolicyPrompt:
		return func(ctx context.Context, req agent.ApprovalRequest) (string, bool, error) {
			return promptViaServer(ctx, taskID, providerName, client, req)
		}
	default: // auto
		return nil
	}
}

// promptViaServer reports an interaction to the server, then polls until
// the user responds or the context is cancelled / times out.
func promptViaServer(ctx context.Context, taskID, providerName string, client *Client, req agent.ApprovalRequest) (string, bool, error) {
	body := map[string]any{
		"type":     req.Type,
		"title":    req.Title,
		"detail":   req.Detail,
		"provider": providerName,
		"options": []map[string]string{
			{"id": "allow", "label": "Allow"},
			{"id": "deny", "label": "Deny"},
		},
		"default_option": "deny",
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
				return chosen, true, nil
			case protocol.InteractionStatusDenied:
				chosen, _ := resp["chosen_option"].(string)
				if chosen == "" {
					chosen = "deny"
				}
				return chosen, false, nil
			case protocol.InteractionStatusTimedOut, protocol.InteractionStatusCancelled:
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
	if store == nil || cb == nil {
		return cb
	}
	return func(ctx context.Context, req agent.ApprovalRequest) (string, bool, error) {
		// Write the approval request to the trace timeline.
		_, _ = store.Append(ctx, trace.TraceLine{
			TaskID:     taskID,
			RunID:      runID,
			Provider:   providerName,
			Channel:    trace.ChannelApprovalRequest,
			Content:    req.Title,
			RawPayload: req.Detail,
		})

		chosen, approved, err := cb(ctx, req)

		// Write the approval response to the trace timeline.
		respContent := fmt.Sprintf("%s (approved=%v)", chosen, approved)
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

		return chosen, approved, err
	}
}
