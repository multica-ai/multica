package handler

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

// TestSpaceResolveMessage locks the single unified wording each service
// space-resolution error maps to, including the guided ambiguous message that
// names the candidate Space keys. Unrecognized errors return "".
func TestSpaceResolveMessage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"not found", service.ErrSpaceNotFound, "space not found in this workspace"},
		{"archived", service.ErrSpaceArchived, "space is archived"},
		{
			"ambiguous names keys",
			&service.ProjectSpaceAmbiguousError{SpaceKeys: []string{"ENG", "GROWTH"}},
			"project has multiple spaces (ENG, GROWTH); specify space_id",
		},
		{"unrelated", errors.New("boom"), ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := spaceResolveMessage(tc.err); got != tc.want {
				t.Fatalf("spaceResolveMessage(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestWriteSpaceResolveError verifies the writer emits a 400 for space-resolution
// errors and reports false (writing nothing) for anything else so callers can
// fall through to their own handling.
func TestWriteSpaceResolveError(t *testing.T) {
	rec := httptest.NewRecorder()
	if !writeSpaceResolveError(rec, service.ErrSpaceNotFound) {
		t.Fatal("expected writeSpaceResolveError to handle ErrSpaceNotFound")
	}
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if writeSpaceResolveError(rec, errors.New("boom")) {
		t.Fatal("expected writeSpaceResolveError to ignore a non-space-resolution error")
	}
	if rec.Code != 200 {
		t.Fatalf("expected nothing written (default 200), got %d", rec.Code)
	}
}
