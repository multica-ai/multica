package handler

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestTeamResolveMessage locks the single unified wording each service
// team-resolution error maps to, including the guided ambiguous message that
// names the candidate Team keys. Unrecognized errors return "".
func TestTeamResolveMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"not found", service.ErrTeamNotFound, "team not found in this workspace"},
		{"archived", service.ErrTeamArchived, "team is archived"},
		{"mismatch", service.ErrProjectTeamMismatch, "project is not associated with this team"},
		{
			"ambiguous names keys",
			&service.ProjectTeamAmbiguousError{TeamKeys: []string{"ENG", "GROWTH"}},
			"project has multiple teams (ENG, GROWTH); specify team_id",
		},
		{"unrelated", errors.New("boom"), ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := teamResolveMessage(tc.err); got != tc.want {
				t.Fatalf("teamResolveMessage(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestWriteTeamResolveError verifies the writer emits a 400 for team-resolution
// errors and reports false (writing nothing) for anything else so callers can
// fall through to their own handling.
func TestWriteTeamResolveError(t *testing.T) {
	rec := httptest.NewRecorder()
	if !writeTeamResolveError(rec, service.ErrTeamNotFound) {
		t.Fatal("expected writeTeamResolveError to handle ErrTeamNotFound")
	}
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if writeTeamResolveError(rec, service.ErrCrossTeamChild) {
		t.Fatal("expected writeTeamResolveError to ignore a non-team-resolution error")
	}
	if rec.Code != 200 {
		t.Fatalf("expected nothing written (default 200), got %d", rec.Code)
	}
}
