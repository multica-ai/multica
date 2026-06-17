package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "modernc.org/sqlite"
)

var kst = time.FixedZone("Asia/Seoul", 9*60*60)

type LLMLimitWindowResponse struct {
	Provider     string  `json:"provider"`
	Window       string  `json:"window"`
	UsedPct      float64 `json:"used_pct"`
	RemainingPct float64 `json:"remaining_pct"`
	ResetAt      string  `json:"reset_at"`
	ResetLabel   string  `json:"reset_label"`
	RecordedAt   string  `json:"recorded_at"`
}

type LLMWeeklyModelResponse struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	RequestCount int64  `json:"request_count"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

type LLMLimitStatusResponse struct {
	Timezone           string                   `json:"timezone"`
	LastRefreshedAt    string                   `json:"last_refreshed_at"`
	SnapshotSource     string                   `json:"snapshot_source"`
	UsageLogSource     string                   `json:"usage_log_source"`
	ResetLabel         string                   `json:"reset_label"`
	WeekProgressPct    int                      `json:"week_progress_pct"`
	WeekDayIndex       int                      `json:"week_day_index"`
	Windows            []LLMLimitWindowResponse `json:"windows"`
	WeeklyModels       []LLMWeeklyModelResponse `json:"weekly_models"`
	Warnings           []string                 `json:"warnings"`
	SonnetUsedPct      *float64                 `json:"sonnet_used_pct,omitempty"`
	SonnetRemainingPct *float64                 `json:"sonnet_remaining_pct,omitempty"`
	SonnetResetAt      string                   `json:"sonnet_reset_at,omitempty"`
	SonnetResetLabel   string                   `json:"sonnet_reset_label,omitempty"`
}

type llmSnapshotRow struct {
	Provider   string
	Window     string
	UsedPct    float64
	ResetAt    time.Time
	RecordedAt time.Time
}

type tokenSnapshotFile struct {
	Timestamp                 string   `json:"timestamp"`
	FiveHourUtilization       *float64 `json:"five_hour_utilization"`
	FiveHourResetsAt          string   `json:"five_hour_resets_at"`
	SevenDayUtilization       *float64 `json:"seven_day_utilization"`
	SevenDayResetsAt          string   `json:"seven_day_resets_at"`
	SevenDaySonnetUtilization *float64 `json:"seven_day_sonnet_utilization"`
	SevenDaySonnetResetsAt    string   `json:"seven_day_sonnet_resets_at"`
}

func (h *Handler) GetDashboardLLMLimitStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	resp := LLMLimitStatusResponse{
		Timezone:        "Asia/Seoul",
		LastRefreshedAt: time.Now().In(kst).Format(time.RFC3339),
		SnapshotSource:  llmStatusDBPath(),
		UsageLogSource:  usageLogSourceLabel(llmUsageDSN()),
		Warnings:        []string{},
	}

	rows, err := readLLMSnapshots(llmStatusDBPath())
	if err != nil {
		resp.Warnings = append(resp.Warnings, "llm_usage_snapshots를 읽지 못했습니다.")
	} else {
		resp.Windows = buildLLMWindows(rows)
	}

	tokenSnapshot, err := readTokenSnapshot(tokenSnapshotPath())
	if err == nil {
		applyTokenSnapshot(&resp, tokenSnapshot)
	}

	anchor := findWeeklyReset(resp.Windows, resp.SonnetResetAt)
	resp.WeekProgressPct, resp.WeekDayIndex, resp.ResetLabel = computeLegacyWeekProgress(time.Now().In(kst), anchor)

	models, err := readWeeklyLLMModels(r.Context(), llmUsageDSN())
	if err != nil {
		resp.Warnings = append(resp.Warnings, "llm_usage_log 주간 집계를 읽지 못했습니다.")
	} else {
		resp.WeeklyModels = models
	}

	writeJSON(w, http.StatusOK, resp)
}

func llmStatusDBPath() string {
	if value := strings.TrimSpace(os.Getenv("NEXAI_LLM_USAGE_DB")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("NEXAI_LLM_STATUS_DB")); value != "" {
		return value
	}
	return "/home/iaas/nexai/state/nexai.db"
}

func tokenSnapshotPath() string {
	if value := strings.TrimSpace(os.Getenv("NEXAI_TOKEN_SNAPSHOT_PATH")); value != "" {
		return value
	}
	return "/home/iaas/nexai/state/token_snapshot.json"
}

func llmUsageDSN() string {
	if value := strings.TrimSpace(os.Getenv("NEXAI_USAGE_DSN")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("NEXAI_LLM_USAGE_PG_DSN"))
}

func usageLogSourceLabel(dsn string) string {
	if strings.TrimSpace(dsn) == "" {
		return ""
	}
	return "nexai_v2.llm_usage_log"
}

func readLLMSnapshots(path string) ([]llmSnapshotRow, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("empty snapshot path")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT provider, window_type, used_pct, reset_at, recorded_at
		  FROM llm_usage_snapshots s
		 WHERE provider IN ('claude', 'openai')
		   AND window_type IN ('5h', '7d')
		   AND recorded_at = (
		       SELECT MAX(recorded_at)
		         FROM llm_usage_snapshots
		        WHERE provider = s.provider
		   )
		 ORDER BY provider, window_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []llmSnapshotRow
	for rows.Next() {
		var provider, windowType, resetRaw, recordedRaw string
		var used float64
		if err := rows.Scan(&provider, &windowType, &used, &resetRaw, &recordedRaw); err != nil {
			return nil, err
		}
		resetAt, err := parseLegacyTime(resetRaw)
		if err != nil {
			return nil, err
		}
		recordedAt, err := parseLegacyTime(recordedRaw)
		if err != nil {
			return nil, err
		}
		out = append(out, llmSnapshotRow{
			Provider: provider, Window: windowType, UsedPct: used, ResetAt: resetAt, RecordedAt: recordedAt,
		})
	}
	return out, rows.Err()
}

func buildLLMWindows(rows []llmSnapshotRow) []LLMLimitWindowResponse {
	out := make([]LLMLimitWindowResponse, 0, len(rows))
	for _, row := range rows {
		used := roundPct(row.UsedPct)
		out = append(out, LLMLimitWindowResponse{
			Provider:     row.Provider,
			Window:       row.Window,
			UsedPct:      used,
			RemainingPct: roundPct(100 - used),
			ResetAt:      row.ResetAt.In(kst).Format(time.RFC3339),
			ResetLabel:   formatResetKSTLabel(row.ResetAt),
			RecordedAt:   row.RecordedAt.In(kst).Format(time.RFC3339),
		})
	}
	return out
}

func readTokenSnapshot(path string) (tokenSnapshotFile, error) {
	var snapshot tokenSnapshotFile
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	err = json.Unmarshal(data, &snapshot)
	return snapshot, err
}

func applyTokenSnapshot(resp *LLMLimitStatusResponse, snapshot tokenSnapshotFile) {
	for i := range resp.Windows {
		window := &resp.Windows[i]
		if window.Provider != "claude" {
			continue
		}
		switch window.Window {
		case "5h":
			if snapshot.FiveHourUtilization != nil {
				window.UsedPct = roundPct(*snapshot.FiveHourUtilization)
				window.RemainingPct = roundPct(100 - window.UsedPct)
			}
			if resetAt, err := parseLegacyTime(snapshot.FiveHourResetsAt); err == nil {
				window.ResetAt = resetAt.In(kst).Format(time.RFC3339)
				window.ResetLabel = formatResetKSTLabel(resetAt)
			}
		case "7d":
			if snapshot.SevenDayUtilization != nil {
				window.UsedPct = roundPct(*snapshot.SevenDayUtilization)
				window.RemainingPct = roundPct(100 - window.UsedPct)
			}
			if resetAt, err := parseLegacyTime(snapshot.SevenDayResetsAt); err == nil {
				window.ResetAt = resetAt.In(kst).Format(time.RFC3339)
				window.ResetLabel = formatResetKSTLabel(resetAt)
			}
		}
	}
	if snapshot.SevenDaySonnetUtilization != nil {
		used := roundPct(*snapshot.SevenDaySonnetUtilization)
		remaining := roundPct(100 - used)
		resp.SonnetUsedPct = &used
		resp.SonnetRemainingPct = &remaining
	}
	if resetAt, err := parseLegacyTime(snapshot.SevenDaySonnetResetsAt); err == nil {
		resp.SonnetResetAt = resetAt.In(kst).Format(time.RFC3339)
		resp.SonnetResetLabel = formatResetKSTLabel(resetAt)
	}
}

func readWeeklyLLMModels(ctx context.Context, dsn string) ([]LLMWeeklyModelResponse, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("empty usage dsn")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT provider,
		       model,
		       COUNT(*)::bigint AS request_count,
		       COALESCE(SUM(tokens_input), 0)::bigint AS input_tokens,
		       COALESCE(SUM(tokens_output), 0)::bigint AS output_tokens,
		       COALESCE(SUM(tokens_total), 0)::bigint AS total_tokens
		  FROM llm_usage_log
		 WHERE recorded_at::timestamptz >= now() - interval '7 days'
		 GROUP BY provider, model
		 ORDER BY total_tokens DESC
		 LIMIT 20
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LLMWeeklyModelResponse
	for rows.Next() {
		var row LLMWeeklyModelResponse
		if err := rows.Scan(&row.Provider, &row.Model, &row.RequestCount, &row.InputTokens, &row.OutputTokens, &row.TotalTokens); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func findWeeklyReset(windows []LLMLimitWindowResponse, sonnetReset string) *time.Time {
	if sonnetReset != "" {
		if resetAt, err := parseLegacyTime(sonnetReset); err == nil {
			return &resetAt
		}
	}
	for _, window := range windows {
		if window.Provider == "claude" && window.Window == "7d" {
			if resetAt, err := parseLegacyTime(window.ResetAt); err == nil {
				return &resetAt
			}
		}
	}
	return nil
}

func computeLegacyWeekProgress(now time.Time, resetAt *time.Time) (int, int, string) {
	nowKST := now.In(kst)
	var hoursUntilReset float64
	if resetAt != nil {
		hoursUntilReset = math.Max(0, resetAt.In(kst).Sub(nowKST).Hours())
		hoursSinceReset := 168 - hoursUntilReset
		progress := int(math.Round(math.Min(hoursSinceReset/168*100, 100)))
		dayIndex := minInt(int(hoursSinceReset/24), 6)
		return progress, dayIndex, resetLabel(hoursUntilReset)
	}

	hoursSinceResetDay := float64(((int(nowKST.Weekday())-5)%7+7)%7*24) + float64(nowKST.Hour()-6) + float64(nowKST.Minute())/60
	if hoursSinceResetDay < 0 {
		hoursSinceResetDay += 168
	}
	hoursUntilReset = 168 - hoursSinceResetDay
	progress := int(math.Round(hoursSinceResetDay / 168 * 100))
	dayIndex := minInt(int(hoursSinceResetDay/24), 6)
	return progress, dayIndex, resetLabel(hoursUntilReset)
}

func resetLabel(hoursUntilReset float64) string {
	daysUntilReset := hoursUntilReset / 24
	if hoursUntilReset < 1 {
		return "곧 리셋"
	}
	if hoursUntilReset < 24 {
		return strconvItoa(int(hoursUntilReset)) + "시간 후 리셋"
	}
	if daysUntilReset < 1.5 {
		return "내일 리셋"
	}
	return strconvItoa(int(daysUntilReset)) + "일 후 리셋"
}

func formatResetKSTLabel(value time.Time) string {
	dt := value.In(kst)
	days := []string{"일", "월", "화", "수", "목", "금", "토"}
	hour := dt.Hour()
	ampm := "오전"
	displayHour := hour
	if hour >= 12 {
		ampm = "오후"
	}
	if hour == 0 {
		displayHour = 12
	} else if hour > 12 {
		displayHour = hour - 12
	}
	return "(" + days[int(dt.Weekday())] + ") " + ampm + " " + strconvItoa(displayHour) + ":" + twoDigits(dt.Minute()) + "에 재설정"
}

func parseLegacyTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty time")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, kst); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid time")
}

func roundPct(value float64) float64 {
	return math.Round(value*10) / 10
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func strconvItoa(value int) string {
	return strconv.FormatInt(int64(value), 10)
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconvItoa(value)
	}
	return strconvItoa(value)
}
