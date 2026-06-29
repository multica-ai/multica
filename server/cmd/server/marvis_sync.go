package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/events"
)

// registerMarvisOfficeSync subscribes to global events and forwards them to the marvis-office Go sync gateway via Webhook.
// This allows low-coupling integration without touching core Multica business logic.
func registerMarvisOfficeSync(bus *events.Bus) {
	bus.SubscribeAll(func(e events.Event) {
		// Only forward events related to Agent status or Issue creation/updates to avoid spamming the Webhook.
		if !strings.HasPrefix(e.Type, "agent.") && !strings.HasPrefix(e.Type, "issue.") {
			return
		}

		payload, err := json.Marshal(e)
		if err != nil {
			return
		}

		go func() {
			// 127.0.0.1:8080 is the default port for the multica-office-sync Go gateway we built earlier.
			resp, err := http.Post("http://127.0.0.1:8080/webhook/multica", "application/json", bytes.NewReader(payload))
			if err != nil {
				slog.Debug("failed to send webhook to marvis-office (is the sync gateway running?)", "error", err)
				return
			}
			resp.Body.Close()
		}()
	})
}
