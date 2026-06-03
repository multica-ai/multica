package duplicatecheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJudgeReturnsNilWhenNoConfig(t *testing.T) {
	// Isolate from any operator env (the dev shell sets these).
	t.Setenv("FIRTAL_REGISTRY_URL", "")
	t.Setenv("FIRTAL_REGISTRY_KEY", "")

	j := &Judger{}
	got := j.Judge(context.Background(), Draft{Title: "x"}, []Candidate{{ID: "c1", Title: "y"}})
	if got != nil {
		t.Fatalf("want nil verdicts when gateway is unconfigured, got %#v", got)
	}
}

func TestJudgeReturnsNilWhenNoCandidates(t *testing.T) {
	j := &Judger{Config: GatewayConfig{BaseURL: "http://x", APIKey: "k"}}
	got := j.Judge(context.Background(), Draft{Title: "x"}, nil)
	if got != nil {
		t.Fatalf("want nil verdicts for empty candidates, got %#v", got)
	}
}

// TestJudgeRoundTrip drives the happy path: the gateway returns one of each
// verdict, the judge parses them, drops verdicts for unknown IDs, and
// preserves the order Haiku returned.
func TestJudgeRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "id=c1") {
			t.Errorf("prompt should mention candidate id=c1, got %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{
					"content": `{"verdicts":[
                        {"id":"c1","verdict":"duplicate","reason":"samme hevo pipeline"},
                        {"id":"c2","verdict":"related","reason":"også hevo, anden pipeline"},
                        {"id":"unknown","verdict":"duplicate","reason":"hallucinated id"}
                    ]}`,
				}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	j := &Judger{
		Config: GatewayConfig{BaseURL: srv.URL, APIKey: "test", Model: JudgeModel},
		Client: srv.Client(),
	}

	got := j.Judge(context.Background(), Draft{Title: "Hevo pipeline fejler"}, []Candidate{
		{ID: "c1", Identifier: "MUL-1", Title: "Hevo pipeline X failure"},
		{ID: "c2", Identifier: "MUL-2", Title: "Hevo pipeline Y retry"},
	})

	if len(got) != 2 {
		t.Fatalf("want 2 verdicts (unknown filtered out), got %d: %#v", len(got), got)
	}
	if got[0].ID != "c1" || got[0].Verdict != VerdictDuplicate {
		t.Errorf("verdict 0: want c1/duplicate, got %s/%s", got[0].ID, got[0].Verdict)
	}
	if got[1].ID != "c2" || got[1].Verdict != VerdictRelated {
		t.Errorf("verdict 1: want c2/related, got %s/%s", got[1].ID, got[1].Verdict)
	}
}

// TestJudgeMalformedResponse: a non-JSON reply means we silently return nil
// (so the create-issue panel just no-shows instead of breaking).
func TestJudgeMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"sorry I can not"}}]}`))
	}))
	defer srv.Close()

	j := &Judger{
		Config: GatewayConfig{BaseURL: srv.URL, APIKey: "test"},
		Client: srv.Client(),
	}
	got := j.Judge(context.Background(), Draft{Title: "x"}, []Candidate{{ID: "c1", Title: "y"}})
	if got != nil {
		t.Fatalf("want nil on malformed body, got %#v", got)
	}
}

// TestJudgeContentAsBlocks covers the Anthropic-shaped reply where content is
// a list of {type, text} blocks. The gateway sometimes passes this through.
func TestJudgeContentAsBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
            "choices":[{"message":{"content":[
                {"type":"text","text":"{\"verdicts\":[{\"id\":\"c1\",\"verdict\":\"related\",\"reason\":\"r\"}]}"}
            ]}}]
        }`))
	}))
	defer srv.Close()

	j := &Judger{
		Config: GatewayConfig{BaseURL: srv.URL, APIKey: "k"},
		Client: srv.Client(),
	}
	got := j.Judge(context.Background(), Draft{Title: "t"}, []Candidate{{ID: "c1", Title: "y"}})
	if len(got) != 1 || got[0].Verdict != VerdictRelated {
		t.Fatalf("want one related verdict, got %#v", got)
	}
}

// TestJudgeSendsBigQueryLabels verifies the three cost-tracking labels
// (deploy-labels-check skill) land on every gateway call, with the user
// label sanitised and an empty UserLabel falling back to "system".
func TestJudgeSendsBigQueryLabels(t *testing.T) {
	for _, tc := range []struct {
		name      string
		userLabel string
		wantUser  string
	}{
		{name: "with user uuid", userLabel: "43501ed6-0b4d-489b-b05e-e5d07e665d91", wantUser: "43501ed6-0b4d-489b-b05e-e5d07e665d91"},
		{name: "with email", userLabel: "jeh@firtal.com", wantUser: "jeh-firtal-com"},
		{name: "empty falls back to system", userLabel: "", wantUser: "system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotApp, gotFunction, gotUser string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotApp = r.Header.Get("x-bq-label-app")
				gotFunction = r.Header.Get("x-bq-label-function")
				gotUser = r.Header.Get("x-bq-label-user")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdicts\":[]}"}}]}`))
			}))
			defer srv.Close()

			j := &Judger{
				Config: GatewayConfig{BaseURL: srv.URL, APIKey: "k"},
				Client: srv.Client(),
			}
			j.Judge(context.Background(),
				Draft{Title: "t", UserLabel: tc.userLabel},
				[]Candidate{{ID: "c1", Title: "y"}},
			)

			if gotApp != "multica" {
				t.Errorf("x-bq-label-app = %q, want multica", gotApp)
			}
			if gotFunction != "duplicate-check" {
				t.Errorf("x-bq-label-function = %q, want duplicate-check", gotFunction)
			}
			if gotUser != tc.wantUser {
				t.Errorf("x-bq-label-user = %q, want %q", gotUser, tc.wantUser)
			}
		})
	}
}

func TestNormalizeVerdictAcceptsDanish(t *testing.T) {
	cases := map[string]Verdict{
		"duplicate":  VerdictDuplicate,
		"DUPLICATE":  VerdictDuplicate,
		"dubletter":  VerdictDuplicate,
		"related":    VerdictRelated,
		"relateret":  VerdictRelated,
		"unrelated":  VerdictUnrelated,
		"urelateret": VerdictUnrelated,
		"":           "",
		"maybe":      "",
	}
	for in, want := range cases {
		if got := normalizeVerdict(in); got != want {
			t.Errorf("normalizeVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerdictIsActionable(t *testing.T) {
	if !VerdictDuplicate.IsActionable() || !VerdictRelated.IsActionable() {
		t.Fatal("duplicate and related should be actionable")
	}
	if VerdictUnrelated.IsActionable() {
		t.Fatal("unrelated must be hidden from the create panel")
	}
}
