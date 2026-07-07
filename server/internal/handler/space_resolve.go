package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// Unified, transport-facing messages for the service-layer space-resolution
// errors. Every issue-producing handler (create, quick-create, autopilot,
// project space lists) maps the same typed error to the same string so callers
// see one wording per condition.
const (
	spaceNotFoundMessage         = "space not found in this workspace"
	spaceArchivedMessage         = "space is archived"
	projectSpaceAmbiguousMessage = "project has multiple spaces; specify space_id"
)

// spaceResolveMessage maps a service space-resolution error to its unified
// message. The ambiguous case includes the candidate Space keys. Returns "" for
// errors that are not space-resolution errors.
func spaceResolveMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrProjectSpaceAmbiguous):
		var amb *service.ProjectSpaceAmbiguousError
		if errors.As(err, &amb) {
			return amb.Error()
		}
		return projectSpaceAmbiguousMessage
	case errors.Is(err, service.ErrSpaceNotFound):
		return spaceNotFoundMessage
	case errors.Is(err, service.ErrSpaceArchived):
		return spaceArchivedMessage
	default:
		return ""
	}
}

// writeSpaceResolveError writes a 400 with the unified message for a
// space-resolution error and returns true. It returns false (writing nothing)
// for errors it does not recognize so callers can fall through to their own
// handling.
func writeSpaceResolveError(w http.ResponseWriter, err error) bool {
	msg := spaceResolveMessage(err)
	if msg == "" {
		return false
	}
	writeError(w, http.StatusBadRequest, msg)
	return true
}
