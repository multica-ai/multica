// Package extension is where commercial packages (server/ee) attach to the
// open-source server. They register from init; the server binary links them
// in through cmd/server/ee.go, and a build without them (-tags oss, or with
// server/ee removed) registers nothing and runs unchanged.
package extension

import (
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/handler"
)

// WorkspaceRoutes mounts routes inside the authenticated, workspace-member
// scope, next to the open-source workspace routes.
type WorkspaceRoutes func(r chi.Router, h *handler.Handler)

var (
	mu              sync.Mutex
	workspaceRoutes []WorkspaceRoutes
)

func RegisterWorkspaceRoutes(mount WorkspaceRoutes) {
	mu.Lock()
	defer mu.Unlock()
	workspaceRoutes = append(workspaceRoutes, mount)
}

func MountWorkspaceRoutes(r chi.Router, h *handler.Handler) {
	mu.Lock()
	mounts := append([]WorkspaceRoutes(nil), workspaceRoutes...)
	mu.Unlock()
	for _, mount := range mounts {
		mount(r, h)
	}
}
