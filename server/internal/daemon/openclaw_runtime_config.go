package daemon

import (
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func decodeOpenclawRuntimeConfig(raw json.RawMessage, logger *slog.Logger) (string, execenv.OpenclawGatewayPin) {
	var payload struct {
		Mode string `json:"mode"`
	}
	_ = json.Unmarshal(raw, &payload)
	return payload.Mode, execenv.OpenclawGatewayPin{}
}

func deriveTaskThreadName(task Task) string {
	if task.ThreadName != "" {
		return task.ThreadName
	}
	if task.Title != "" {
		return task.Title
	}
	return task.ID
}
