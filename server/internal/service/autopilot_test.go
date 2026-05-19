package service

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotErrorType(t *testing.T) {
	cases := map[string]string{
		"unknown execution_mode: nope": "configuration",
		"issue blocked":                "issue_terminal",
		"issue cancelled":              "issue_terminal",
		"enqueue task: no runtime":     "dispatch_error",
		"task failed":                  "task_error",
		"unexpected":                   "autopilot_error",
	}

	for reason, want := range cases {
		if got := autopilotErrorType(reason); got != want {
			t.Fatalf("autopilotErrorType(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestBuildIssueDescription_NoTriggerPayload(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "do the thing", Valid: true}}
	run := db.AutopilotRun{Source: "schedule"}

	got := s.buildIssueDescription(ap, run)
	if !strings.HasPrefix(got.String, "do the thing") {
		t.Fatalf("description should preserve user description: %q", got.String)
	}
	if !strings.Contains(got.String, "Autopilot run triggered at") {
		t.Fatalf("description should include schedule note: %q", got.String)
	}
	if strings.Contains(got.String, "Webhook event") {
		t.Fatalf("description must not mention webhook for non-webhook source: %q", got.String)
	}
}

func TestBuildIssueDescription_WithWebhookPayload(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "watch PRs", Valid: true}}
	payload := []byte(`{"event":"github.pull_request.opened","eventPayload":{"number":7},"request":{"receivedAt":"2026-05-09T00:00:00Z","contentType":"application/json"}}`)
	run := db.AutopilotRun{Source: "webhook", TriggerPayload: payload}

	got := s.buildIssueDescription(ap, run)
	if !strings.HasPrefix(got.String, "watch PRs") {
		t.Fatalf("user description not preserved: %q", got.String)
	}
	if !strings.Contains(got.String, "Webhook event: github.pull_request.opened") {
		t.Fatalf("description should include webhook event line: %q", got.String)
	}
	if !strings.Contains(got.String, "\"number\": 7") && !strings.Contains(got.String, "\"number\":7") {
		t.Fatalf("description should include payload json: %q", got.String)
	}
	// Italic schedule line must come before the webhook block.
	idxItalic := strings.Index(got.String, "*Autopilot run triggered")
	idxWebhook := strings.Index(got.String, "Webhook event")
	if idxItalic < 0 || idxWebhook < 0 || idxItalic > idxWebhook {
		t.Fatalf("italic line should appear before webhook block: %q", got.String)
	}
}

func TestBuildIssueDescription_WebhookSourceMissingEnvelope(t *testing.T) {
	// Defensive: if a future caller stuffs a non-envelope JSON object into
	// trigger_payload, we should still emit a webhook block with sensible
	// defaults rather than skipping the section entirely.
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "thing", Valid: true}}
	payload := []byte(`{"raw":"missing envelope"}`)
	run := db.AutopilotRun{Source: "webhook", TriggerPayload: payload}

	got := s.buildIssueDescription(ap, run)
	if !strings.Contains(got.String, "Webhook event:") {
		t.Fatalf("should still emit webhook block: %q", got.String)
	}
}

func TestBuildIssueDescription_NonWebhookSourceWithPayloadIgnored(t *testing.T) {
	// Manual / schedule with a payload should not get a webhook block.
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "thing", Valid: true}}
	run := db.AutopilotRun{Source: "manual", TriggerPayload: []byte(`{"event":"x.y"}`)}

	got := s.buildIssueDescription(ap, run)
	if strings.Contains(got.String, "Webhook event") {
		t.Fatalf("non-webhook source should not include webhook block: %q", got.String)
	}
}

// TestInterpolateTemplate covers the behaviours that real autopilot runs
// depend on: {{date}} substitution, {{trigger_time}} substitution pinned to
// the run's triggered_at (P0 — TUB-191), falling back to Title when the
// template is unset/empty, and leaving any unknown {{...}} text alone (the
// handler is the layer that prevents unknown tokens from being stored in
// the first place — service-layer interpolation stays substitute-or-leave).
func TestInterpolateTemplate(t *testing.T) {
	s := &AutopilotService{}
	triggeredAt := time.Date(2026, 4, 30, 6, 41, 0, 0, time.UTC)
	run := db.AutopilotRun{TriggeredAt: pgtype.Timestamptz{Time: triggeredAt, Valid: true}}

	cases := []struct {
		name   string
		ap     db.Autopilot
		expect string
	}{
		{
			name:   "date placeholder substituted",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "probe — {{date}}", Valid: true}},
			expect: "probe — 2026-04-30",
		},
		{
			name:   "date placeholder with whitespace substituted",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "probe — {{ date }}", Valid: true}},
			expect: "probe — 2026-04-30",
		},
		{
			name:   "trigger_time placeholder substituted with UTC timestamp",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "Production Health Check — {{trigger_time}}", Valid: true}},
			expect: "Production Health Check — 2026-04-30 06:41 UTC",
		},
		{
			name:   "trigger_time placeholder tolerates whitespace inside braces",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "probe — {{ trigger_time }}", Valid: true}},
			expect: "probe — 2026-04-30 06:41 UTC",
		},
		{
			name:   "date and trigger_time in same template both render",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "{{date}} — {{trigger_time}}", Valid: true}},
			expect: "2026-04-30 — 2026-04-30 06:41 UTC",
		},
		{
			name:   "empty template falls back to autopilot title",
			ap:     db.Autopilot{Title: "fallback title", IssueTitleTemplate: pgtype.Text{Valid: false}},
			expect: "fallback title",
		},
		{
			name:   "template without placeholder is returned verbatim",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "static title", Valid: true}},
			expect: "static title",
		},
		{
			name:   "trigger_time renders against fallback Title too",
			ap:     db.Autopilot{Title: "fallback — {{trigger_time}}", IssueTitleTemplate: pgtype.Text{Valid: false}},
			expect: "fallback — 2026-04-30 06:41 UTC",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.interpolateTemplate(tc.ap, run); got != tc.expect {
				t.Fatalf("interpolateTemplate = %q, want %q", got, tc.expect)
			}
		})
	}
}

// TestInterpolateTemplate_TriggerTimeMatchesDescriptionFooter pins the P0
// regression: when an autopilot puts {{trigger_time}} in its title and the
// description footer renders a timestamp from the same run, the two strings
// must agree. A future refactor that drifts the two renderers will fail here.
func TestInterpolateTemplate_TriggerTimeMatchesDescriptionFooter(t *testing.T) {
	s := &AutopilotService{}
	triggeredAt := time.Date(2026, 4, 30, 6, 41, 0, 0, time.UTC)
	run := db.AutopilotRun{
		Source:      "schedule",
		TriggeredAt: pgtype.Timestamptz{Time: triggeredAt, Valid: true},
	}
	ap := db.Autopilot{
		Title:              "Production Health Check",
		IssueTitleTemplate: pgtype.Text{String: "Production Health Check — {{trigger_time}}", Valid: true},
		Description:        pgtype.Text{String: "probe", Valid: true},
	}

	title := s.interpolateTemplate(ap, run)
	desc := s.buildIssueDescription(ap, run).String

	const want = "2026-04-30 06:41 UTC"
	if !strings.Contains(title, want) {
		t.Fatalf("title should contain %q, got %q", want, title)
	}
	if !strings.Contains(desc, want) {
		t.Fatalf("description footer should contain %q, got %q", want, desc)
	}
}

// TestValidateIssueTitleTemplate locks down what create/update accept.
// Reject path: anything inside {{...}} that is not in the supported set.
// Accept path: empty, plain text, and the canonical {{date}} placeholder
// in both compact and whitespace-padded forms.
func TestValidateIssueTitleTemplate(t *testing.T) {
	t.Run("accepts empty template", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate(""); err != nil {
			t.Fatalf("empty template must be valid: %v", err)
		}
	})
	t.Run("accepts plain text", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate("daily report"); err != nil {
			t.Fatalf("plain text must be valid: %v", err)
		}
	})
	t.Run("accepts {{date}}", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate("probe — {{date}}"); err != nil {
			t.Fatalf("{{date}} must be valid: %v", err)
		}
	})
	t.Run("accepts {{ date }} with whitespace", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate("probe — {{ date }}"); err != nil {
			t.Fatalf("{{ date }} must be valid: %v", err)
		}
	})
	t.Run("accepts {{trigger_time}}", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate("probe — {{trigger_time}}"); err != nil {
			t.Fatalf("{{trigger_time}} must be valid: %v", err)
		}
	})
	t.Run("accepts {{ trigger_time }} with whitespace", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate("probe — {{ trigger_time }}"); err != nil {
			t.Fatalf("{{ trigger_time }} must be valid: %v", err)
		}
	})
	t.Run("accepts {{date}} and {{trigger_time}} together", func(t *testing.T) {
		if err := ValidateIssueTitleTemplate("{{date}} — {{trigger_time}}"); err != nil {
			t.Fatalf("combined template must be valid: %v", err)
		}
	})

	rejections := []struct {
		name string
		tmpl string
		// nameInError is the offending variable name that must appear in the
		// returned error so CLI users see which token was rejected.
		nameInError string
	}{
		{"go template style", "probe — {{.TriggeredAt}}", ".TriggeredAt"},
		{"mustache style unknown variable", "probe — {{trigger_id}}", "trigger_id"},
		{"datetime not yet supported", "probe — {{datetime}}", "datetime"},
		{"empty placeholder", "probe — {{}}", ""},
		{"mixed valid + invalid still fails", "probe — {{date}} {{trigger_source}}", "trigger_source"},
	}
	for _, tc := range rejections {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIssueTitleTemplate(tc.tmpl)
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.tmpl)
			}
			if !strings.Contains(err.Error(), "unknown template variable") {
				t.Fatalf("error should mention unknown template variable: %v", err)
			}
			if tc.nameInError != "" && !strings.Contains(err.Error(), tc.nameInError) {
				t.Fatalf("error should name the offending token %q: %v", tc.nameInError, err)
			}
		})
	}
}
