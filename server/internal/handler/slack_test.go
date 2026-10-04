package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A successful BYO install must broadcast slack_installation:created so all open
// clients (not just the installer's tab) invalidate the installations query —
// the regression Niko's review caught (RegisterSlackBYO previously only wrote
// the response). Bus.Publish is synchronous, so the subscriber fires inline.
func TestPublishSlackInstallationCreated(t *testing.T) {
	bus := events.New()
	h := &Handler{Bus: bus}

	const (
		wsID   = "11111111-1111-1111-1111-111111111111"
		instID = "22222222-2222-2222-2222-222222222222"
	)

	var got events.Event
	fired := 0
	bus.Subscribe(protocol.EventSlackInstallationCreated, func(e events.Event) {
		got = e
		fired++
	})

	h.publishSlackInstallationCreated(db.ChannelInstallation{
		ID:          parseUUID(instID),
		WorkspaceID: parseUUID(wsID),
	}, "user-1")

	if fired != 1 {
		t.Fatalf("expected slack_installation:created published once, got %d", fired)
	}
	if got.WorkspaceID != wsID || got.ActorType != "user" || got.ActorID != "user-1" {
		t.Errorf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["id"] != instID {
		t.Errorf("payload = %v, want installation id %s", got.Payload, instID)
	}
}

func TestSlackInstallationResponseReplyPolicy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg, _ := json.Marshal(map[string]any{
			"dm_replies_in_threads": enabled,
			"bot_token_encrypted":   "secret-bot", "app_token_encrypted": "secret-app",
		})
		out := slackInstallationToResponse(db.ChannelInstallation{Config: cfg})
		if out.DMRepliesInThreads != enabled {
			t.Fatalf("reply policy = %t, want %t", out.DMRepliesInThreads, enabled)
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "secret-") {
			t.Fatal("public installation response leaked credentials")
		}
	}
}
