package middleware

import (
	"net/http"

	"github.com/google/uuid"
)

// Provenance captures the author identity for audit/revision records.
// Shared by workspace documents and Skills 2.0.
type Provenance struct {
	AuthorType string     // "human", "agent_foreground", "agent_background", "import"
	AuthorID   *uuid.UUID // user or agent UUID
	TaskID     *uuid.UUID // agent_task_queue.id when running inside a task
}

// ProvenanceFromRequest inspects the request to determine who is acting.
//
//   - mdt_* daemon token  → agent_foreground (or agent_background if
//     X-Curator-Run header is set)
//   - mul_*/JWT user token → human
//
// task_id comes from the X-Multica-Task-ID header (set by the daemon when
// subprocess CLI calls run as part of a known task).
func ProvenanceFromRequest(r *http.Request) Provenance {
	p := Provenance{}

	// Determine author type from auth path set by DaemonAuth / Auth middleware.
	authPath := DaemonAuthPathFromContext(r.Context())

	switch authPath {
	case DaemonAuthPathDaemonToken:
		p.AuthorType = "agent_foreground"
		if r.Header.Get("X-Curator-Run") != "" {
			p.AuthorType = "agent_background"
		}
	default:
		// PAT, JWT, or any other user auth path
		p.AuthorType = "human"
	}

	// Author ID: for human tokens it's the user ID set by Auth middleware;
	// for daemon tokens we don't have a direct user ID — leave nil (the
	// daemon is acting on behalf of the agent, not a user). The daemon-token
	// branch of DaemonAuth does NOT overwrite client-supplied X-User-ID
	// (unlike the PAT/JWT branches), so we must gate on authPath here.
	// Without this filter, anyone holding a daemon token could spoof an
	// arbitrary user UUID in the provenance / revision history audit trail.
	if authPath != DaemonAuthPathDaemonToken {
		if userID := r.Header.Get("X-User-ID"); userID != "" {
			if uid, err := uuid.Parse(userID); err == nil {
				p.AuthorID = &uid
			}
		}
	}

	// Task ID from header (daemon sets this when a subprocess CLI runs).
	if taskHeader := r.Header.Get("X-Multica-Task-ID"); taskHeader != "" {
		if tid, err := uuid.Parse(taskHeader); err == nil {
			p.TaskID = &tid
		}
	}

	return p
}
