package notetypes

// FIR-3589 item 6: the Cycles page re-times a recurring note by sending only
// the recurrence fields. That must not take the note's template or icon with
// it — before this guard, a PUT without template_body silently blanked it.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUpdateNoteType_RetimingKeepsTemplateAndIcon(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)

	q := cerebrodb.New(applyTestPool)
	created, err := q.CreateCerebroNoteType(ctx, cerebrodb.CreateCerebroNoteTypeParams{
		WorkspaceID:      applyTestWorkspaceID,
		Name:             "Business Review",
		Icon:             "calendar",
		TemplateBody:     "## Review {{periode}}\n- Numbers:",
		RecurrenceMode:   ModeNewNote,
		CadenceUnit:      CadenceWeek,
		CadenceCount:     1,
		Enabled:          true,
		CreatedBy:        applyTestUserID,
		NumberingEnabled: false,
		NextNumber:       1,
	})
	if err != nil {
		t.Fatalf("create note type: %v", err)
	}

	// Exactly what the Cycles editor sends when only the rhythm changes.
	body, err := json.Marshal(map[string]any{
		"name":                 "Business Review",
		"recurrence_mode":      ModeNewNote,
		"cadence_unit":         CadenceMonth,
		"cadence_count":        1,
		"anchor_weekday":       1,
		"anchor_week_of_month": 3,
		"participants":         []map[string]string{},
		"enabled":              true,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	id := util.UUIDToString(created.ID)
	req := httptest.NewRequest(http.MethodPut, "/api/cerebro/note-types/"+id, bytes.NewReader(body))
	req.Header.Set("X-User-ID", util.UUIDToString(applyTestUserID))
	reqCtx := middleware.SetMemberContext(req.Context(), util.UUIDToString(applyTestWorkspaceID), db.Member{ID: applyTestUserID})
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(reqCtx, chi.RouteCtxKey, routeCtx))

	rec := httptest.NewRecorder()
	NewHandler(q, applyTestPool).UpdateNoteType(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	after, err := q.GetCerebroNoteType(ctx, created.ID)
	if err != nil {
		t.Fatalf("re-read note type: %v", err)
	}
	if after.TemplateBody != created.TemplateBody {
		t.Errorf("template body = %q, want it left alone (%q)", after.TemplateBody, created.TemplateBody)
	}
	if after.Icon != created.Icon {
		t.Errorf("icon = %q, want it left alone (%q)", after.Icon, created.Icon)
	}
	if after.CadenceUnit != CadenceMonth || !after.AnchorWeekOfMonth.Valid || after.AnchorWeekOfMonth.Int16 != 3 {
		t.Errorf("re-timing did not stick: cadence=%s week_of_month=%+v", after.CadenceUnit, after.AnchorWeekOfMonth)
	}
}
