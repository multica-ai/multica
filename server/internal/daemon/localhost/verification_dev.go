//go:build verificationdev

package localhost

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/localconversation/service"
)

func (v *V2) verificationRoute(w http.ResponseWriter, r *http.Request, c caller, scopeID int64, path string) bool {
	if !strings.HasPrefix(path, "/verification/") {
		return false
	}
	if !service.VerificationEnabled() || c.kind != callerMain {
		http.NotFound(w, r)
		return true
	}
	switch {
	case path == "/verification/capabilities" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"schemaVersion": 1, "generation": v.svc.Generation(), "capabilities": []string{"snapshot", "events", "import-local-conversation-v1", "fixture"}})
	case path == "/verification/events" && r.Method == http.MethodGet:
		id := r.URL.Query().Get("conversationId")
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil || after < 0 {
			writeV2Error(w, AppError("invalid_request", "A nonnegative event cursor is required"))
			return true
		}
		snapshot, err := v.svc.Snapshot(r.Context(), scopeID, id, 1)
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		if r.URL.Query().Get("generation") != v.svc.Generation() || after > snapshot.SnapshotSeq {
			writeV2Error(w, AppError("snapshot_required", "The source generation or cursor changed"))
			return true
		}
		events, err := v.svc.Store().EventsAfter(r.Context(), id, after, snapshot.SnapshotSeq, 500)
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		next := after
		for _, event := range events {
			if event.Seq > next {
				next = event.Seq
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"schemaVersion": 1, "generation": v.svc.Generation(), "events": eventDTOs(events), "cursor": next, "highWater": snapshot.SnapshotSeq, "hasMore": next < snapshot.SnapshotSeq})
	case path == "/verification/ws" && r.Method == http.MethodGet:
		id := r.URL.Query().Get("conversationId")
		after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if err != nil || after < 0 || r.URL.Query().Get("generation") != v.svc.Generation() {
			writeV2Error(w, AppError("snapshot_required", "Invalid observation cursor"))
			return true
		}
		sub, high, err := v.svc.Subscribe(r.Context(), scopeID, id, after)
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		defer sub.Close()
		connection, err := v.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return true
		}
		defer connection.Close()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		connection.SetReadLimit(1024)
		go func() {
			for {
				if _, _, err := connection.ReadMessage(); err != nil {
					cancel()
					return
				}
			}
		}()
		if err := connection.WriteJSON(map[string]any{"type": "observation.ready", "generation": v.svc.Generation(), "cursor": high}); err != nil {
			return true
		}
		for {
			events, err := sub.Next(ctx)
			if err != nil {
				return true
			}
			rows := make([]map[string]any, len(events))
			for index, event := range events {
				rows[index] = eventDTO(event)
			}
			if err := connection.WriteJSON(map[string]any{"generation": v.svc.Generation(), "events": rows}); err != nil {
				return true
			}
		}
	case path == "/verification/import" && r.Method == http.MethodPost:
		var body service.VerificationData
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20))
		if err := decoder.Decode(&body); err != nil {
			writeV2Error(w, AppError("invalid_request", "Invalid verification data protocol"))
			return true
		}
		mapping, err := v.svc.VerificationImport(r.Context(), scopeID, body)
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"schemaVersion": 1, "mapping": mapping, "policy": "frozen-history-paused-queue", "transformations": []string{"providerSessionsNotResumed", "runningTurnsCancelled", "queuesPaused", "workingDirectoryRebound", "cloudReferencesRemoved", "copiesUnarchived"}})
	case path == "/verification/fixture" && r.Method == http.MethodPost:
		var body struct {
			ConversationID    string `json:"conversationId"`
			Action            string `json:"action"`
			Text              string `json:"text"`
			ExpectedRequestID string `json:"expectedRequestId"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeV2Error(w, AppError("invalid_request", "Invalid fixture control"))
			return true
		}
		result, err := v.svc.VerificationFixture(r.Context(), scopeID, body.ConversationID, body.Action, body.Text, body.ExpectedRequestID)
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
	case path == "/verification/fixture" && r.Method == http.MethodGet:
		result, err := v.svc.VerificationFixture(r.Context(), scopeID, r.URL.Query().Get("conversationId"), "inspect", "", "")
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
	default:
		http.NotFound(w, r)
	}
	return true
}
