package sessions

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUpdateSessionModeRoundTripsAndPersistsWhenOmitted(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	rootID := seedRootComment(t, issueID, workspaceID)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rec := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Planning","mode":"plan"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set mode: code=%d body=%s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var first sessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &first)
	if first.Mode != "plan" {
		t.Fatalf("mode = %q, want plan", first.Mode)
	}

	rec = callUpdate(h, issueID, workspaceID, rootID, `{"name":"Planning renamed"}`)
	var second sessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &second)
	if second.Mode != "plan" {
		t.Fatalf("mode after omitted PATCH = %q, want plan", second.Mode)
	}
}

func TestUpdateSessionAcceptsAllCanonicalModesAndLegacyDefault(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	rootID := seedRootComment(t, issueID, workspaceID)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	for _, tc := range []struct {
		input string
		want  string
	}{
		{"auto", "auto"},
		{"plan", "plan"},
		{"build", "build"},
		{"research", "research"},
		{"review", "review"},
		{"default", "build"},
	} {
		rec := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Mode","mode":"`+tc.input+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %q: code=%d body=%s", tc.input, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
		var got sessionResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.Mode != tc.want {
			t.Fatalf("mode %q normalized to %q, want %q", tc.input, got.Mode, tc.want)
		}
	}
}

func TestUpdateSessionRejectsUnknownMode(t *testing.T) {
	issueID, workspaceID := seedIssue(t)
	rootID := seedRootComment(t, issueID, workspaceID)
	h := NewHandler(sessTestPool, db.New(sessTestPool), nil, nil)

	rec := callUpdate(h, issueID, workspaceID, rootID, `{"name":"Bad","mode":"ship"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s, want 400", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}
