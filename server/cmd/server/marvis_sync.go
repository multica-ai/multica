package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
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
			webhookURL := os.Getenv("MARVIS_WEBHOOK_URL")
			if webhookURL == "" {
				// Use host.docker.internal so Docker containers can reach the host machine.
				// Use port 8081 to avoid conflict with Multica's default 8080 backend.
				webhookURL = "http://host.docker.internal:8081/webhook/multica"
			}
			resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(payload))
			if err != nil {
				slog.Debug("failed to send webhook to marvis-office (is the sync gateway running?)", "error", err, "url", webhookURL)
				return
			}
			resp.Body.Close()
		}()
	})
}
