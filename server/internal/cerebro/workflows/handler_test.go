package workflows

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestValidateWriteRequest_RequiresName(t *testing.T) {
	err := validateWriteRequest(writeWorkflowRequest{
		TriggerType: TriggerStatusChanged,
		ActionType:  ActionSetStatus,
	})
	if err == nil {
		t.Fatal("empty name must be rejected")
	}
}

func TestValidateWriteRequest_RejectsUnknownEnumValues(t *testing.T) {
	cases := []struct {
		name    string
		req     writeWorkflowRequest
		wantErr bool
	}{
		{
			name: "known trigger and action",
			req: writeWorkflowRequest{
				Name:        "x",
				TriggerType: TriggerStatusChanged,
				ActionType:  ActionCreateSubIssue,
			},
		},
		{
			name: "unknown trigger",
			req: writeWorkflowRequest{
				Name:        "x",
				TriggerType: "issue_starred",
				ActionType:  ActionSetStatus,
			},
			wantErr: true,
		},
		{
			name: "unknown action",
			req: writeWorkflowRequest{
				Name:        "x",
				TriggerType: TriggerStatusChanged,
				ActionType:  "post_to_slack",
			},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateWriteRequest(c.req)
			if c.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestKnownEnums(t *testing.T) {
	for _, tt := range []string{
		TriggerStatusChanged, TriggerDueDateReached, TriggerDueTimeReached,
		TriggerCron, TriggerWebhookInbound, TriggerCommentMention,
		TriggerAllChildrenDone, TriggerSubIssueCreated,
	} {
		if !knownTrigger(tt) {
			t.Errorf("knownTrigger(%q) = false, want true", tt)
		}
	}
	for _, a := range []string{
		ActionSetStatus, ActionCreateSubIssue, ActionSendReminder,
		ActionRunSkill, ActionCommentOnIssue,
		ActionRouteByDomain,
		ActionWebhookOutbound, ActionReassignIssue,
	} {
		if !knownAction(a) {
			t.Errorf("knownAction(%q) = false, want true", a)
		}
	}
	if knownTrigger("") || knownAction("") {
		t.Fatal("empty string must not be a known trigger or action")
	}
}

func TestKnownEditorMode(t *testing.T) {
	for _, m := range []string{EditorModeForm, EditorModeCanvas} {
		if !knownEditorMode(m) {
			t.Errorf("knownEditorMode(%q) = false, want true", m)
		}
	}
	if knownEditorMode("") || knownEditorMode("zapier") {
		t.Fatal("empty and unknown values must be rejected")
	}
}

func TestValidateWriteRequest_AcceptsPhase2Actions(t *testing.T) {
	cases := []string{ActionRunSkill, ActionCommentOnIssue}
	for _, a := range cases {
		err := validateWriteRequest(writeWorkflowRequest{
			Name:        "x",
			TriggerType: TriggerStatusChanged,
			ActionType:  a,
		})
		if err != nil {
			t.Errorf("validateWriteRequest must accept %q, got %v", a, err)
		}
	}
}

func TestValidateWriteRequest_AcceptsPhase2ExtActions(t *testing.T) {
	for _, a := range []string{ActionRouteByDomain} {
		err := validateWriteRequest(writeWorkflowRequest{
			Name:        "x",
			TriggerType: TriggerStatusChanged,
			ActionType:  a,
		})
		if err != nil {
			t.Errorf("validateWriteRequest must accept %q, got %v", a, err)
		}
	}
}

func TestValidateWriteRequest_RejectsUnknownEditorMode(t *testing.T) {
	err := validateWriteRequest(writeWorkflowRequest{
		Name:        "x",
		TriggerType: TriggerStatusChanged,
		ActionType:  ActionSetStatus,
		EditorMode:  "zapier",
	})
	if err == nil {
		t.Fatal("unknown editor_mode must be rejected")
	}
}

func TestDefaultJSON(t *testing.T) {
	if got := string(defaultJSON(nil, "{}")); got != "{}" {
		t.Errorf("nil input must use fallback, got %q", got)
	}
	if got := string(defaultJSON([]byte(`{"a":1}`), "{}")); got != `{"a":1}` {
		t.Errorf("non-empty input must pass through, got %q", got)
	}
}

// --- Phase-3 (JEH-1108, PR 2) extensions ---

func TestValidateWriteRequest_RejectsBadCronSchedule(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigCron{ScheduleExpr: "not a cron"})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:          "x",
		TriggerType:   TriggerCron,
		TriggerConfig: cfg,
		ActionType:    ActionSetStatus,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid cron") {
		t.Fatalf("expected invalid cron error, got %v", err)
	}
}

func TestValidateWriteRequest_AcceptsValidCronSchedule(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigCron{ScheduleExpr: "@hourly", Timezone: "Europe/Copenhagen"})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:          "x",
		TriggerType:   TriggerCron,
		TriggerConfig: cfg,
		ActionType:    ActionSetStatus,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestValidateWriteRequest_RejectsBadCommentRegex(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigCommentMention{
		MatchMode: CommentMatchRegex, Target: "[unclosed",
	})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:          "x",
		TriggerType:   TriggerCommentMention,
		TriggerConfig: cfg,
		ActionType:    ActionSetStatus,
	})
	if err == nil || !strings.Contains(err.Error(), "regex") {
		t.Fatalf("expected regex error, got %v", err)
	}
}

func TestValidateWriteRequest_RejectsHTTPWebhookURL(t *testing.T) {
	t.Setenv("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST", "")
	cfg, _ := json.Marshal(ActionConfigWebhookOutbound{URL: "http://example.com/hook"})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:         "x",
		TriggerType:  TriggerStatusChanged,
		ActionType:   ActionWebhookOutbound,
		ActionConfig: cfg,
	})
	if err == nil || !strings.Contains(err.Error(), "scheme must be https") {
		t.Fatalf("expected scheme rejection, got %v", err)
	}
}

func TestValidateWriteRequest_RejectsForbiddenWebhookHeader(t *testing.T) {
	cfg, _ := json.Marshal(ActionConfigWebhookOutbound{
		URL:     "https://example.com/hook",
		Headers: map[string]string{"Authorization": "Bearer x"},
	})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:         "x",
		TriggerType:  TriggerStatusChanged,
		ActionType:   ActionWebhookOutbound,
		ActionConfig: cfg,
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden header") {
		t.Fatalf("expected forbidden header, got %v", err)
	}
}

func TestValidateWriteRequest_RejectsMultiPrefixWebhookHeader(t *testing.T) {
	cfg, _ := json.Marshal(ActionConfigWebhookOutbound{
		URL:     "https://example.com/hook",
		Headers: map[string]string{"X-Multica-Trigger": "fake"},
	})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:         "x",
		TriggerType:  TriggerStatusChanged,
		ActionType:   ActionWebhookOutbound,
		ActionConfig: cfg,
	})
	if err == nil || !strings.Contains(err.Error(), "forbidden header") {
		t.Fatalf("expected forbidden header, got %v", err)
	}
}

func TestValidateWriteRequest_WebhookMissingURL(t *testing.T) {
	cfg, _ := json.Marshal(ActionConfigWebhookOutbound{})
	err := validateWriteRequest(writeWorkflowRequest{
		Name:         "x",
		TriggerType:  TriggerStatusChanged,
		ActionType:   ActionWebhookOutbound,
		ActionConfig: cfg,
	})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected url-required error, got %v", err)
	}
}

func TestToWorkflowResponse_MasksSecrets(t *testing.T) {
	row := cerebrodb.CerebroWorkflow{
		ID:                    mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		WorkspaceID:           mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Name:                  "Test",
		Enabled:               true,
		TriggerType:           TriggerWebhookInbound,
		ActionType:            ActionSetStatus,
		InboundWebhookToken:   pgtype.Text{String: "tok_abc", Valid: true},
		InboundSigningSecret:  pgtype.Text{String: "shh-secret", Valid: true},
		OutboundWebhookSecret: pgtype.Text{String: "out-secret", Valid: true},
	}
	resp := toWorkflowResponse(row)
	if resp.InboundWebhookToken != "tok_abc" {
		t.Errorf("token must be visible, got %q", resp.InboundWebhookToken)
	}
	if !resp.InboundSigningSecretSet {
		t.Error("inbound_signing_secret_set must be true when secret is set")
	}
	if !resp.OutboundWebhookSecretSet {
		t.Error("outbound_webhook_secret_set must be true when secret is set")
	}
	// JSON output must not contain the secrets in plaintext.
	encoded, _ := json.Marshal(resp)
	if strings.Contains(string(encoded), "shh-secret") {
		t.Fatalf("inbound signing secret leaked into response JSON:\n%s", encoded)
	}
	if strings.Contains(string(encoded), "out-secret") {
		t.Fatalf("outbound webhook secret leaked into response JSON:\n%s", encoded)
	}
}

func TestToWorkflowResponse_AbsentSecrets(t *testing.T) {
	row := cerebrodb.CerebroWorkflow{
		ID:          mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		WorkspaceID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Name:        "Test",
		Enabled:     true,
		TriggerType: TriggerStatusChanged,
		ActionType:  ActionSetStatus,
	}
	resp := toWorkflowResponse(row)
	if resp.InboundWebhookToken != "" {
		t.Errorf("expected empty token, got %q", resp.InboundWebhookToken)
	}
	if resp.InboundSigningSecretSet {
		t.Error("set bool must be false when secret unset")
	}
	if resp.OutboundWebhookSecretSet {
		t.Error("set bool must be false when secret unset")
	}
}

func TestGenerateOpaqueToken_LengthAndUniqueness(t *testing.T) {
	a, err := generateOpaqueToken(32)
	if err != nil {
		t.Fatalf("rand failed: %v", err)
	}
	b, err := generateOpaqueToken(32)
	if err != nil {
		t.Fatalf("rand failed: %v", err)
	}
	if a == b {
		t.Fatal("two generated tokens must not collide")
	}
	if len(a) < 40 || len(a) > 50 {
		t.Fatalf("expected ~43-char base64 token, got len=%d (%q)", len(a), a)
	}
	if strings.ContainsAny(a, "+/=") {
		t.Errorf("token must be URL-safe-base64 (no +/=), got %q", a)
	}
}

func TestPublicWebhookURL_BuildShape(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://prod.example.com")
	t.Setenv("FRONTEND_ORIGIN", "")
	req := httptest.NewRequest(http.MethodPost, "/api/cerebro/workflows/x/regenerate-token", nil)
	got := publicWebhookURL(req, "TOKEN123")
	want := "https://prod.example.com/api/cerebro/workflows/webhook/TOKEN123"
	if got != want {
		t.Errorf("publicWebhookURL = %q, want %q", got, want)
	}
}

func TestPublicWebhookURL_DerivesFromRequest(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "")
	t.Setenv("FRONTEND_ORIGIN", "")
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/cerebro/workflows/x/regenerate-token", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Host = "api.example.com"
	got := publicWebhookURL(req, "TOK")
	want := "https://api.example.com/api/cerebro/workflows/webhook/TOK"
	if got != want {
		t.Errorf("publicWebhookURL = %q, want %q", got, want)
	}
}
