package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// A supporting-file write is a change to the skill: it must bump the skill's
// updated_at and announce skill:updated with the full file set, exactly like
// PUT /api/skills/{id} does. Before this, clients holding the skill never
// learned that a file had changed.
func TestSkillFileWritesPublishSkillUpdated(t *testing.T) {
	skillID := insertHandlerTestSkill(t, "skill-file-events", "body")

	var mu sync.Mutex
	var updates []SkillWithFilesResponse
	testHandler.Bus.Subscribe(protocol.EventSkillUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		skill, ok := payload["skill"].(SkillWithFilesResponse)
		if !ok || skill.ID != skillID {
			return
		}
		mu.Lock()
		updates = append(updates, skill)
		mu.Unlock()
	})
	lastUpdate := func() (SkillWithFilesResponse, int) {
		mu.Lock()
		defer mu.Unlock()
		if len(updates) == 0 {
			return SkillWithFilesResponse{}, 0
		}
		return updates[len(updates)-1], len(updates)
	}
	updatedAt := func() time.Time {
		var ts time.Time
		if err := testPool.QueryRow(context.Background(),
			`SELECT updated_at FROM skill WHERE id = $1`, skillID).Scan(&ts); err != nil {
			t.Fatalf("read updated_at: %v", err)
		}
		return ts
	}

	before := updatedAt()

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/skills/"+skillID+"/files", map[string]any{
		"path":    "notes/a.md",
		"content": "alpha",
	})
	req = withURLParam(req, "id", skillID)
	testHandler.UpsertSkillFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpsertSkillFile: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got, n := lastUpdate()
	if n != 1 {
		t.Fatalf("expected 1 skill:updated after upsert, got %d", n)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "notes/a.md" || got.Files[0].Content != "alpha" {
		t.Fatalf("upsert event files = %+v, want the one upserted file", got.Files)
	}
	afterUpsert := updatedAt()
	if !afterUpsert.After(before) {
		t.Fatalf("upsert did not bump skill.updated_at: before %v, after %v", before, afterUpsert)
	}

	fileID := got.Files[0].ID
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/skills/"+skillID+"/files/"+fileID, nil)
	req = withURLParams(req, "id", skillID, "fileId", fileID)
	testHandler.DeleteSkillFile(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteSkillFile: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	got, n = lastUpdate()
	if n != 2 {
		t.Fatalf("expected 2 skill:updated after delete, got %d", n)
	}
	if len(got.Files) != 0 {
		t.Fatalf("delete event files = %+v, want none", got.Files)
	}
	if !updatedAt().After(afterUpsert) {
		t.Fatal("delete did not bump skill.updated_at")
	}
}
