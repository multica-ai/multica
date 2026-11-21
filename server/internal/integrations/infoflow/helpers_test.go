package infoflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dolfly/infoflow/bot/events"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// testBox returns a deterministic secretbox for tests.
func testBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

// payloadFromJSON decodes a JSON object with json.Number preserved, matching how
// the SDK's dispatcher decodes transport frames.
func payloadFromJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return m
}

// eventFromPayload runs a payload through the SDK's real dispatcher so the
// resulting events.Event carries the same ChatType / MessageType the runtime
// would produce, then returns the captured event.
func eventFromPayload(t *testing.T, raw string) events.Event {
	t.Helper()
	var captured events.Event
	d := events.NewDispatcher(events.Options{})
	if err := d.On("*", func(_ context.Context, ev events.Event) error {
		captured = ev
		return nil
	}); err != nil {
		t.Fatalf("On: %v", err)
	}
	if err := d.DispatchFrame(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("DispatchFrame: %v", err)
	}
	if captured.Type == "" {
		t.Fatalf("event was not dispatched for payload %s", raw)
	}
	return captured
}

// mustUUID parses a UUID string, failing the test on error.
func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return u
}
