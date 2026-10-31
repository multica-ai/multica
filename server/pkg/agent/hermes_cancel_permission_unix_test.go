//go:build unix

package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func sendHermesCancelPermission(send func(any)) {
	send(map[string]any{
		"jsonrpc": "2.0", "id": 9001, "method": "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_cancel_target",
			"toolCall":  map[string]any{"toolCallId": "new-write", "title": "write_file", "kind": "edit"},
			"options": []map[string]any{
				{"optionId": "allow_once", "kind": "allow_once", "name": "Allow once"},
				{"optionId": "allow_always", "kind": "allow_always", "name": "Allow always"},
				{"optionId": "deny", "kind": "reject_once", "name": "Deny"},
			},
		},
	})
}

// Exercise the real Backend.Execute transport and process lifecycle, not just
// permission selection. The fake peer writes a file only after receiving a grant.
func TestHermesPermissionAfterCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, mode, status string
		timeout            time.Duration
	}{
		{"parent", "permission_cancel", "aborted", 0},
		{"timeout", "permission_cancel", "timeout", 1500 * time.Millisecond},
		{"normal", "permission_normal", "completed", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, logPath := startHermesCancelFake(t, ctx, tc.mode, tc.timeout)
			waitHermesCancelLog(t, logPath, `"method":"session/prompt"`)
			if tc.name == "parent" {
				cancel()
			}
			result := finishHermesCancelFake(t, session)
			if result.Status != tc.status {
				t.Errorf("status = %q, want %q", result.Status, tc.status)
			}
			if usage := result.Usage["unknown"]; usage.InputTokens != 7 || usage.OutputTokens != 3 {
				t.Errorf("lost original prompt response usage: %+v", result.Usage)
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			replies := 0
			for _, line := range strings.Split(string(raw), "\n") {
				var frame struct {
					ID     int `json:"id"`
					Result struct {
						Outcome map[string]any `json:"outcome"`
					} `json:"result"`
				}
				if json.Unmarshal([]byte(line), &frame) != nil || frame.ID != 9001 {
					continue
				}
				replies++
				outcome := frame.Result.Outcome
				if tc.name == "normal" {
					if outcome["outcome"] != "selected" || outcome["optionId"] != "allow_once" {
						t.Errorf("normal permission changed: %+v", outcome)
					}
				} else if outcome["outcome"] != "cancelled" || len(outcome) != 1 {
					t.Errorf("permission after cancellation = %+v, want only outcome:cancelled", outcome)
				}
			}
			if replies != 1 {
				t.Errorf("permission replies = %d, want 1", replies)
			}
			_, err = os.Stat(logPath + ".effect")
			if tc.name == "normal" {
				if err != nil {
					t.Errorf("normal permission did not permit side effect: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("new side effect after cancellation: stat err = %v", err)
			}
			assertHermesCancelWire(t, logPath, tc.name != "normal", true)
		})
	}
}
