package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestMatchDestructiveCommand(t *testing.T) {
	t.Parallel()

	blocked := []string{
		"rm -rf /",
		"rm -fr /",
		"sudo rm -rf / ",
		"git push --force origin main",
		"git push -f origin master",
		"git push --force-with-lease origin main",
		"DROP DATABASE production",
		"drop table users;",
		"TRUNCATE TABLE events",
		":(){ :|:& };:",
	}
	for _, cmd := range blocked {
		if matchDestructiveCommand(cmd) == "" {
			t.Errorf("expected block: %q", cmd)
		}
	}
	allowed := []string{
		"rm -rf ./build",
		"rm build.log",
		"git push origin feature/main-rename",
		"git push --force origin feature-branch",
		"git commit -m 'drop old code'",
		"ls /",
	}
	for _, cmd := range allowed {
		if pat := matchDestructiveCommand(cmd); pat != "" {
			t.Errorf("unexpected block %q via %q", cmd, pat)
		}
	}
}

// destructiveGateCase drives handleLine the same way
// TestHermesClientAutoApprovesPermissionRequest does, asserting the gate
// denies destructive toolCall titles before any auto-grant selector runs.
func TestHermesClientBlocksDestructivePermissionRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		title   string
		options string
		wantErr bool   // true → JSON-RPC error reply, no outcome
		wantID  string // expected selected optionId when wantErr == false
	}{
		{
			name:    "rm -rf / denied via offered reject_once",
			title:   "rm -rf /",
			options: `[{"optionId":"allow_once","kind":"allow_once"},{"optionId":"deny","kind":"reject_once"}]`,
			wantID:  "deny",
		},
		{
			name:    "force push main denied, error when no reject offered",
			title:   "git push --force origin main",
			options: `[{"optionId":"allow_once","kind":"allow_once"}]`,
			wantErr: true,
		},
		{
			name:    "DROP DATABASE denied",
			title:   "psql -c 'DROP DATABASE production'",
			options: `[{"optionId":"deny","kind":"reject_once"}]`,
			wantID:  "deny",
		},
		{
			name:    "benign title still auto-grants",
			title:   "write: reply.md",
			options: `[{"optionId":"allow_session","kind":"allow_always"}]`,
			wantID:  "allow_session",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &bufferWriter{}
			c := &hermesClient{
				cfg:     Config{Logger: slog.Default()},
				stdin:   w,
				pending: make(map[int]*pendingRPC),
			}

			c.handleLine(`{"jsonrpc":"2.0","id":42,"method":"session/request_permission","params":{"sessionId":"ses_1","options":` + tc.options + `,"toolCall":{"toolCallId":"tc_1","title":"` + tc.title + `","content":[]}}}`)

			var resp struct {
				Result *struct {
					Outcome struct {
						OptionID string `json:"optionId"`
					} `json:"outcome"`
				} `json:"result"`
				Error *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(w.String())), &resp); err != nil {
				t.Fatalf("reply is not valid JSON: %q err=%v", w.String(), err)
			}
			if tc.wantErr {
				if resp.Error == nil {
					t.Fatalf("want JSON-RPC error reply, got result %+v", resp.Result)
				}
				if !strings.Contains(resp.Error.Message, "blocked pending human approval") {
					t.Errorf("error message should name the gate, got %q", resp.Error.Message)
				}
				return
			}
			if resp.Error != nil {
				t.Fatalf("unexpected JSON-RPC error reply: %+v", resp.Error)
			}
			if resp.Result.Outcome.OptionID != tc.wantID {
				t.Errorf("optionId: got %q, want %q", resp.Result.Outcome.OptionID, tc.wantID)
			}
		})
	}
}
