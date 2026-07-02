package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// Unified, transport-facing messages for the service-layer team-resolution
// errors. Every issue-producing handler (create, quick-create, autopilot,
// project team lists) maps the same typed error to the same string so callers
// see one wording per condition.
const (
	teamNotFoundMessage         = "team not found in this workspace"
	teamArchivedMessage         = "team is archived"
	projectTeamMismatchMessage  = "project is not associated with this team"
	projectTeamAmbiguousMessage = "project has multiple teams; specify team_id"
)

// teamResolveMessage maps a service team-resolution error to its unified
// message. The ambiguous case includes the candidate Team keys. Returns "" for
// errors that are not team-resolution errors.
func teamResolveMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrProjectTeamAmbiguous):
		var amb *service.ProjectTeamAmbiguousError
		if errors.As(err, &amb) {
			return amb.Error()
		}
		return projectTeamAmbiguousMessage
	case errors.Is(err, service.ErrTeamNotFound):
		return teamNotFoundMessage
	case errors.Is(err, service.ErrTeamArchived):
		return teamArchivedMessage
	case errors.Is(err, service.ErrProjectTeamMismatch):
		return projectTeamMismatchMessage
	default:
		return ""
	}
}

// writeTeamResolveError writes a 400 with the unified message for a
// team-resolution error and returns true. It returns false (writing nothing)
// for errors it does not recognize so callers can fall through to their own
// handling.
func writeTeamResolveError(w http.ResponseWriter, err error) bool {
	msg := teamResolveMessage(err)
	if msg == "" {
		return false
	}
	writeError(w, http.StatusBadRequest, msg)
	return true
}
