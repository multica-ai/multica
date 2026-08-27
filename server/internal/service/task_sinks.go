package service

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ponytail: GAP-6 (#9) outbound notification sinks. Fire-and-forget POST,
// no retry/HMAC/queue. Add HMAC signing + the webhook_delivery worker path
// if a deployment needs guaranteed delivery.

// NotifySinksFromEnv parses MULTICA_NOTIFY_SINKS (comma-separated URLs).
// Empty/unset means disabled — zero behavior change for default deployments.
func NotifySinksFromEnv() []string {
	var sinks []string
	for _, raw := range strings.Split(os.Getenv("MULTICA_NOTIFY_SINKS"), ",") {
		if u := strings.TrimSpace(raw); u != "" {
			sinks = append(sinks, u)
		}
	}
	return sinks
}

type notifySinkPayload struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

var notifySinkClient = &http.Client{Timeout: 2 * time.Second}

// dispatchNotifySinks posts one JSON payload per configured sink. Called from
// terminal task transitions (NotifyTaskFinished); best-effort and async so a
// slow or dead sink never blocks the request path.
func (s *TaskService) dispatchNotifySinks(task db.AgentTaskQueue) {
	for _, sink := range s.NotifySinks {
		go postTaskEvent(sink, task.ID, task.Status, firstText(task.FailureReason, task.Error))
	}
}

func postTaskEvent(sinkURL string, taskID pgtype.UUID, status string, reason string) {
	payload, err := json.Marshal(notifySinkPayload{TaskID: util.UUIDToString(taskID), Status: status, Reason: reason})
	if err != nil {
		return
	}
	resp, err := notifySinkClient.Post(sinkURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		slog.Warn("notify sink delivery failed", "sink", sinkURL, "task_id", util.UUIDToString(taskID), "error", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("notify sink rejected event", "sink", sinkURL, "task_id", util.UUIDToString(taskID), "status", resp.StatusCode)
	}
}

// firstText returns the first pgtype.Text that is valid and non-empty.
func firstText(vals ...pgtype.Text) string {
	for _, v := range vals {
		if v.Valid && v.String != "" {
			return v.String
		}
	}
	return ""
}
