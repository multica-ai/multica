//go:build verificationdev

package localhost

import (
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
		writeJSON(w, http.StatusOK, map[string]any{"schemaVersion": 1, "mapping": mapping, "policy": "frozen-history-paused-queue", "transformations": []string{"providerSessionsNotResumed", "runningTurnsCancelled", "queuesPaused", "inFlightSubmissionsNeedReview", "workingDirectoryRebound", "cloudReferencesRemoved", "copiesUnarchived"}})
	case path == "/verification/fixture" && r.Method == http.MethodPost:
		var body struct {
			ConversationID    string                    `json:"conversationId"`
			Action            string                    `json:"action"`
			Text              string                    `json:"text"`
			ExpectedRequestID string                    `json:"expectedRequestId"`
			Tool              *service.VerificationTool `json:"tool"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeV2Error(w, AppError("invalid_request", "Invalid fixture control"))
			return true
		}
		result, err := v.svc.VerificationFixture(r.Context(), scopeID, body.ConversationID, body.Action, body.Text, body.ExpectedRequestID, body.Tool)
		if err != nil {
			writeV2Error(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
	case path == "/verification/fixture" && r.Method == http.MethodGet:
		result, err := v.svc.VerificationFixture(r.Context(), scopeID, r.URL.Query().Get("conversationId"), "inspect", "", "", nil)
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
