package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type recordingStatusNotifier struct{ ids []pgtype.UUID }

func (r *recordingStatusNotifier) NotifyStatusChanged(_ context.Context, id pgtype.UUID) error {
	r.ids = append(r.ids, id)
	return nil
}

func TestIssueCallbackListenerFiresOnStatusChangesOnly(t *testing.T) {
	bus := events.New()
	rec := &recordingStatusNotifier{}
	registerIssueCallbackListener(bus, rec, func(f func()) { f() })
	const id = "7d1e5a43-5b0a-4d4c-9c8e-6f5f2d2b8a11"

	publish := func(payload map[string]any) {
		bus.Publish(events.Event{Type: protocol.EventIssueUpdated, Payload: payload})
	}
	publish(map[string]any{"issue": handler.IssueResponse{ID: id, Status: "done"}, "status_changed": true})
	publish(map[string]any{"issue": map[string]any{"id": id, "status": "in_review"}, "status_changed": true})
	publish(map[string]any{"issue": handler.IssueResponse{ID: id, Status: "done"}, "title_changed": true})
	publish(map[string]any{"issue": handler.IssueResponse{ID: "not-a-uuid", Status: "done"}, "status_changed": true})

	want, _ := util.ParseUUID(id)
	if len(rec.ids) != 2 || rec.ids[0] != want || rec.ids[1] != want {
		t.Fatalf("notified %v, want the issue twice (both payload shapes)", rec.ids)
	}
}
