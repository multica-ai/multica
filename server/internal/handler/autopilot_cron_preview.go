package handler

import (
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

// Rejection codes let the editor say which input is at fault: a timezone the
// server's tzdata does not know is not the user's cron being wrong, and the two
// are fixed in different controls.
const (
	cronPreviewInvalidCron     = "invalid_cron"
	cronPreviewInvalidTimezone = "invalid_timezone"
)

func writeCronPreviewError(w http.ResponseWriter, code, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg, "code": code})
}

// CronPreview computes the next occurrences of a candidate cron expression
// so schedule editors can show an authoritative preview before saving.
// Compute-only: workspace membership is enforced by the router group and no
// other resource is touched.
func (h *Handler) CronPreview(w http.ResponseWriter, r *http.Request) {
	expr := r.URL.Query().Get("expr")
	if expr == "" {
		writeCronPreviewError(w, cronPreviewInvalidCron, "expr is required")
		return
	}
	tz := r.URL.Query().Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	if err := service.ValidateTimezone(tz); err != nil {
		writeCronPreviewError(w, cronPreviewInvalidTimezone, err.Error())
		return
	}

	const previewCount = 3
	// A syntactically valid expression that never fires comes back as a short (or
	// empty) slice, not an error: the editor tells "never runs" from "your cron is
	// wrong" by the status code.
	occurrences, err := service.NextOccurrencesAfterUTC(expr, tz, time.Now().UTC(), previewCount)
	if err != nil {
		writeCronPreviewError(w, cronPreviewInvalidCron, err.Error())
		return
	}
	nextRuns := make([]string, 0, len(occurrences))
	for _, at := range occurrences {
		nextRuns = append(nextRuns, at.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_runs": nextRuns})
}
