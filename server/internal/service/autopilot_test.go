package service

import (
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

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

func TestTaskFailureReasonForAutopilotRun(t *testing.T) {
	cases := []struct {
		name string
		task db.AgentTaskQueue
		want string
	}{
		{
			name: "prefers raw error text",
			task: db.AgentTaskQueue{
				Error:         pgtype.Text{String: "tests failed", Valid: true},
				FailureReason: pgtype.Text{String: "agent_error", Valid: true},
			},
			want: "tests failed",
		},
		{
			name: "falls back to classified reason when error is blank",
			task: db.AgentTaskQueue{
				Error:         pgtype.Text{String: "   ", Valid: true},
				FailureReason: pgtype.Text{String: "codex_semantic_inactivity", Valid: true},
			},
			want: "codex_semantic_inactivity",
		},
		{
			name: "generic default when nothing is set",
			task: db.AgentTaskQueue{},
			want: "task failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskFailureReasonForAutopilotRun(tc.task); got != tc.want {
				t.Fatalf("taskFailureReasonForAutopilotRun() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildIssueDescription_NoTriggerPayload(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "do the thing", Valid: true}}
	run := db.AutopilotRun{Source: "schedule", TriggeredAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}

	got := s.buildIssueDescription(ap, run, "UTC")
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

func TestBuildIssueDescription_UsesTriggerTimezone(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "daily sync", Valid: true}}
	run := db.AutopilotRun{
		Source:      "schedule",
		TriggeredAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC), Valid: true},
	}

	got := s.buildIssueDescription(ap, run, "Asia/Tokyo")
	if !strings.Contains(got.String, "Autopilot run triggered at 2026-05-26 09:00 Asia/Tokyo") {
		t.Fatalf("description should use trigger timezone: %q", got.String)
	}
	if strings.Contains(got.String, "2026-05-26 00:00 UTC") {
		t.Fatalf("description must not render the trigger time in UTC when trigger timezone is known: %q", got.String)
	}
}

// An invalid IANA timezone string must fall back to UTC instead of leaving the
// timestamp half-formatted in the issue body.
func TestBuildIssueDescription_InvalidTriggerTimezoneFallsBackToUTC(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "do the thing", Valid: true}}
	run := db.AutopilotRun{
		Source:      "schedule",
		TriggeredAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC), Valid: true},
	}

	got := s.buildIssueDescription(ap, run, "Foo/Bar")
	if !strings.Contains(got.String, "Autopilot run triggered at 2026-05-26 00:00 UTC") {
		t.Fatalf("invalid trigger timezone should fall back to UTC: %q", got.String)
	}
}

func TestInterpolateTemplate_InvalidTriggerTimezoneFallsBackToUTC(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Title:              "fallback",
		IssueTitleTemplate: pgtype.Text{String: "report {{date}}", Valid: true},
	}
	run := db.AutopilotRun{
		TriggeredAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC), Valid: true},
	}

	got := s.interpolateTemplate(ap, run, "Foo/Bar")
	if want := "report 2026-05-26"; got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestBuildIssueDescription_WithWebhookPayload(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "watch PRs", Valid: true}}
	payload := []byte(`{"event":"github.pull_request.opened","eventPayload":{"number":7},"request":{"receivedAt":"2026-05-09T00:00:00Z","contentType":"application/json"}}`)
	run := db.AutopilotRun{Source: "webhook", TriggerPayload: payload, TriggeredAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}

	got := s.buildIssueDescription(ap, run, "UTC")
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
	run := db.AutopilotRun{Source: "webhook", TriggerPayload: payload, TriggeredAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}

	got := s.buildIssueDescription(ap, run, "UTC")
	if !strings.Contains(got.String, "Webhook event:") {
		t.Fatalf("should still emit webhook block: %q", got.String)
	}
}

func TestBuildIssueDescription_NonWebhookSourceWithPayloadIgnored(t *testing.T) {
	// Manual / schedule with a payload should not get a webhook block.
	s := &AutopilotService{}
	ap := db.Autopilot{Description: pgtype.Text{String: "thing", Valid: true}}
	run := db.AutopilotRun{Source: "manual", TriggerPayload: []byte(`{"event":"x.y"}`), TriggeredAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}

	got := s.buildIssueDescription(ap, run, "UTC")
	if strings.Contains(got.String, "Webhook event") {
		t.Fatalf("non-webhook source should not include webhook block: %q", got.String)
	}
}

// TestInterpolateTemplate covers the shared substitution contract: supported
// variables render, an unset template falls back to Title, and plain text is
// preserved. The handler prevents unknown tokens from being stored; the
// service renderer still leaves them intact for backward compatibility.
func TestInterpolateTemplate(t *testing.T) {
	s := &AutopilotService{}
	run := db.AutopilotRun{TriggeredAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}}
	today := run.TriggeredAt.Time.UTC().Format("2006-01-02")

	cases := []struct {
		name   string
		ap     db.Autopilot
		expect string
	}{
		{
			name:   "date placeholder substituted",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "probe — {{date}}", Valid: true}},
			expect: "probe — " + today,
		},
		{
			name:   "date placeholder with whitespace substituted",
			ap:     db.Autopilot{Title: "fallback", IssueTitleTemplate: pgtype.Text{String: "probe — {{ date }}", Valid: true}},
			expect: "probe — " + today,
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.interpolateTemplate(tc.ap, run, "UTC"); got != tc.expect {
				t.Fatalf("interpolateTemplate = %q, want %q", got, tc.expect)
			}
		})
	}
}

func TestInterpolateTemplate_UsesTriggerTimezoneForDate(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Title:              "fallback",
		IssueTitleTemplate: pgtype.Text{String: "Tokyo report {{date}}", Valid: true},
	}
	run := db.AutopilotRun{
		TriggeredAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 26, 23, 30, 0, 0, time.UTC), Valid: true},
	}

	got := s.interpolateTemplate(ap, run, "Asia/Tokyo")
	if want := "Tokyo report 2026-05-27"; got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestInterpolateTemplate_UsesPerDeliveryWebhookVariables(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Title: "fallback",
		IssueTitleTemplate: pgtype.Text{
			String: "{{event}} #{{payload.number}} big={{payload.big_number}} {{payload.repository.full_name}} draft={{payload.draft}}",
			Valid:  true,
		},
	}
	run := db.AutopilotRun{
		Source: "webhook",
		TriggerPayload: []byte(`{
			"event":"github.pull_request.opened",
			"eventPayload":{
				"number":42,
				"big_number":9007199254740993,
				"repository":{"full_name":"multica-ai/multica"},
				"draft":false
			}
		}`),
	}

	got := s.interpolateTemplate(ap, run, "UTC")
	want := "github.pull_request.opened #42 big=9007199254740993 multica-ai/multica draft=false"
	if got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestInterpolateTemplate_UsesTriggerTimezoneForDateTimeAndTime(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Title: "fallback",
		IssueTitleTemplate: pgtype.Text{
			String: "{{date}} {{datetime}} {{time}}",
			Valid:  true,
		},
	}
	run := db.AutopilotRun{
		TriggeredAt: pgtype.Timestamptz{Time: time.Date(2026, 5, 26, 23, 30, 45, 0, time.UTC), Valid: true},
	}

	got := s.interpolateTemplate(ap, run, "Asia/Tokyo")
	want := "2026-05-27 2026-05-27T08:30:45+09:00 08:30:45"
	if got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestInterpolateTemplate_MissingAndNonScalarPayloadValuesRenderEmpty(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		IssueTitleTemplate: pgtype.Text{
			String: "title={{payload.title}} missing={{payload.missing}} object={{payload.object}} list={{payload.list}} null={{payload.null}}",
			Valid:  true,
		},
	}
	run := db.AutopilotRun{
		Source: "webhook",
		TriggerPayload: []byte(`{
			"event":"demo.received",
			"eventPayload":{
				"title":"  Line one\nline two  ",
				"object":{"value":"hidden"},
				"list":["hidden"],
				"null":null
			}
		}`),
	}

	got := s.interpolateTemplate(ap, run, "UTC")
	want := "title=Line one line two missing= object= list= null="
	if got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestInterpolateTemplate_WebhookVariablesRenderEmptyForOtherSources(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		IssueTitleTemplate: pgtype.Text{String: "event={{event}} payload={{payload.title}}", Valid: true},
	}
	run := db.AutopilotRun{
		Source:         "manual",
		TriggerPayload: []byte(`{"event":"demo.received","eventPayload":{"title":"must not leak"}}`),
	}

	got := s.interpolateTemplate(ap, run, "UTC")
	want := "event= payload="
	if got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestInterpolateTemplate_BoundsDynamicValues(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		IssueTitleTemplate: pgtype.Text{String: "{{payload.title}}", Valid: true},
	}
	run := db.AutopilotRun{
		Source:         "webhook",
		TriggerPayload: []byte(`{"event":"demo.received","eventPayload":{"title":"` + strings.Repeat("界", 500) + `"}}`),
	}

	got := s.interpolateTemplate(ap, run, "UTC")
	const wantMaxRunes = 200
	if len([]rune(got)) != wantMaxRunes {
		t.Fatalf("dynamic value length = %d, want %d", len([]rune(got)), wantMaxRunes)
	}
}

func TestInterpolateTemplate_TruncatedDynamicValuesRemainDistinct(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		IssueTitleTemplate: pgtype.Text{String: "{{payload.title}}", Valid: true},
	}
	prefix := strings.Repeat("a", issueTitleTemplateValueMaxRunes+20)
	render := func(suffix string) string {
		run := db.AutopilotRun{
			Source:         "webhook",
			TriggerPayload: []byte(`{"event":"demo.received","eventPayload":{"title":"` + prefix + suffix + `"}}`),
		}
		return s.interpolateTemplate(ap, run, "UTC")
	}

	first := render("-first")
	second := render("-second")
	if first == second {
		t.Fatalf("distinct values collapsed to the same bounded title %q", first)
	}
	if got := len([]rune(first)); got != issueTitleTemplateValueMaxRunes {
		t.Fatalf("first title length = %d, want %d", got, issueTitleTemplateValueMaxRunes)
	}
	if got := render("-first"); got != first {
		t.Fatalf("bounded title is not deterministic: first=%q second=%q", first, got)
	}
}

func TestInterpolateTemplate_ControlCharactersDoNotReachTitle(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		IssueTitleTemplate: pgtype.Text{String: "{{payload.title}}", Valid: true},
	}
	run := db.AutopilotRun{
		Source:         "webhook",
		TriggerPayload: []byte(`{"event":"demo.received","eventPayload":{"title":"hello\u0000world\u001b[31m"}}`),
	}

	got := s.interpolateTemplate(ap, run, "UTC")
	want := "hello world [31m"
	if got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
}

func TestInterpolateTemplate_FormatControlsDoNotReachTitle(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		IssueTitleTemplate: pgtype.Text{String: "{{payload.title}}", Valid: true},
	}
	run := db.AutopilotRun{
		Source:         "webhook",
		TriggerPayload: []byte(`{"event":"demo.received","eventPayload":{"title":"safe\u202eevil\u2066\u200btext"}}`),
	}

	got := s.interpolateTemplate(ap, run, "UTC")
	want := "safeeviltext"
	if got != want {
		t.Fatalf("interpolateTemplate = %q, want %q", got, want)
	}
	if strings.IndexFunc(got, func(r rune) bool { return unicode.Is(unicode.Cf, r) }) >= 0 {
		t.Fatalf("rendered title contains a Unicode format control: %q", got)
	}
}

func TestInterpolateTemplate_EmptyDynamicTitleFallsBackToAutopilotTitle(t *testing.T) {
	s := &AutopilotService{}
	ap := db.Autopilot{
		Title:              "Generic webhook issue",
		IssueTitleTemplate: pgtype.Text{String: " {{payload.missing}} ", Valid: true},
	}
	run := db.AutopilotRun{
		Source:         "webhook",
		TriggerPayload: []byte(`{"event":"demo.received","eventPayload":{}}`),
	}

	if got := s.interpolateTemplate(ap, run, "UTC"); got != ap.Title {
		t.Fatalf("interpolateTemplate = %q, want fallback title %q", got, ap.Title)
	}
}

func FuzzInterpolateTemplate_WebhookPayloadNeverLeaksInvalidOrControlText(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"event":"ticket.updated","eventPayload":{"title":"ABC-123"}}`),
		[]byte(`{"event":"demo.received","eventPayload":{"title":"hello\u0000world\u001b[31m"}}`),
		[]byte(`{"event":"demo.received","eventPayload":{"title":9007199254740993}}`),
		[]byte(`{"event":"demo.received","eventPayload":{"title":{"nested":true}}}`),
		[]byte(`not-json`),
		nil,
	} {
		f.Add(seed)
	}

	s := &AutopilotService{}
	ap := db.Autopilot{
		Title:              "Fallback title",
		IssueTitleTemplate: pgtype.Text{String: "event={{event}} title={{payload.title}} again={{payload.title}}", Valid: true},
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		run := db.AutopilotRun{Source: "webhook", TriggerPayload: payload}
		got := s.interpolateTemplate(ap, run, "UTC")
		if !utf8.ValidString(got) {
			t.Fatalf("rendered title is not valid UTF-8: %q", got)
		}
		if strings.IndexFunc(got, unicode.IsControl) >= 0 {
			t.Fatalf("rendered title contains a control character: %q", got)
		}
		if strings.IndexFunc(got, func(r rune) bool { return unicode.Is(unicode.Cf, r) }) >= 0 {
			t.Fatalf("rendered title contains a Unicode format control: %q", got)
		}
		if runes := len([]rune(got)); runes > 3*issueTitleTemplateValueMaxRunes+32 {
			t.Fatalf("rendered title unexpectedly large: %d runes", runes)
		}
	})
}

// TestValidateIssueTitleTemplate locks down what create/update accept.
// Reject path: anything inside {{...}} that is not in the supported set.
// Accept path: empty, plain text, fixed variables, and validated payload paths.
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
	for _, tmpl := range []string{
		"report {{datetime}} at {{time}}",
		"event {{event}}",
		"ticket {{payload.identifier}}",
		"repo {{payload.repository.full_name}}",
	} {
		t.Run("accepts "+tmpl, func(t *testing.T) {
			if err := ValidateIssueTitleTemplate(tmpl); err != nil {
				t.Fatalf("%q must be valid: %v", tmpl, err)
			}
		})
	}

	rejections := []struct {
		name string
		tmpl string
		// nameInError is the offending variable name that must appear in the
		// returned error so CLI users see which token was rejected.
		nameInError string
	}{
		{"go template style", "probe — {{.TriggeredAt}}", ".TriggeredAt"},
		{"mustache style unknown variable", "probe — {{trigger_id}}", "trigger_id"},
		{"empty placeholder", "probe — {{}}", ""},
		{"mixed valid + invalid still fails", "probe — {{date}} {{trigger_source}}", "trigger_source"},
		{"payload prefix without field", "probe — {{payload}}", "payload"},
		{"payload field missing", "probe — {{payload.}}", "payload."},
		{"payload field has empty segment", "probe — {{payload.repo..name}}", "payload.repo..name"},
		{"payload field has spaces", "probe — {{payload.ticket title}}", "payload.ticket title"},
		{"payload documentation placeholder", "probe — {{payload.<field>}}", "payload.<field>"},
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
