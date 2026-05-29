// CEREBRO-PATCH(daemonws-tool-scan-now): FIR-2230 — server→daemon push for an
// admin-triggered immediate MCP tools/list scan. Net-new fork file; lives in
// the daemonws package so it can reuse the hub's unexported delivery path
// (notifyFrame) without widening the hub's public surface or touching the
// upstream hub.go.
//
// Best-effort, same channel as a task-available wakeup. The empty event ID
// disables dedup so repeated admin "Scan now" clicks each reach the daemon.
// The admin handler is expected to check RuntimeConnectionCount first, so a
// dropped frame here means the daemon disconnected between the check and the
// send — the next scheduled heartbeat scan still covers that case.
package daemonws

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// RequestToolScan pushes an immediate tools/list scan request to the daemon(s)
// serving runtimeID.
func (h *Hub) RequestToolScan(runtimeID string) {
	if h == nil || runtimeID == "" {
		return
	}
	data, err := toolScanRequestedFrame(runtimeID)
	if err != nil {
		return
	}
	h.notifyFrame(runtimeID, data, "")
}

func toolScanRequestedFrame(runtimeID string) ([]byte, error) {
	return json.Marshal(protocol.Message{
		Type: protocol.EventDaemonToolScanRequested,
		Payload: mustMarshalRaw(protocol.ToolScanRequestedPayload{
			RuntimeID: runtimeID,
		}),
	})
}
