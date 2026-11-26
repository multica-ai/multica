package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// daemon:register fans out to every member of the workspace, so it must not
// carry runtime rows: a private runtime's details are only readable by the
// members its owner allows.
func TestDaemonRegister_RefreshEventCarriesNoRuntimeRows(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	daemonID := "register-event-" + uuid.NewString()
	runtimeID := dbfx.Runtime(t, "private register event runtime", testutil.Cols{
		"daemon_id": daemonID, "provider": "claude", "runtime_mode": "local",
		"owner_id": testUserID, "visibility": "private",
	})

	h := *testHandler
	h.Bus = events.New()
	var refreshes []events.Event
	h.Bus.Subscribe(protocol.EventDaemonRegister, func(event events.Event) {
		refreshes = append(refreshes, event)
	})

	testutil.Call(t, h.DaemonRegister, registerAs(testUserID, daemonID)).Want(http.StatusOK)

	if len(refreshes) != 1 {
		t.Fatalf("daemon:register events = %d, want 1", len(refreshes))
	}
	payload, ok := refreshes[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want a map", refreshes[0].Payload)
	}
	if len(payload) != 1 || payload["action"] != "register" {
		t.Fatalf("workspace-wide event payload = %#v, want only {action: register} (private runtime %s)", payload, runtimeID)
	}
}
