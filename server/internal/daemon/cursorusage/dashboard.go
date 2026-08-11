package cursorusage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	cursorAPIBase           = "https://cursor.com"
	filteredUsageEventsPath = "/api/dashboard/get-filtered-usage-events"
	defaultHTTPTimeout      = 12 * time.Second
	defaultFilteredPageSize = 200
)

// UsageEvent is one Cursor Dashboard usage row (undocumented shape).
type UsageEvent struct {
	Timestamp        time.Time
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	// ChargedCents is Cursor's metered spend for the event. Only meaningful
	// when HasChargedCents is true — a present zero is a real included-in-plan
	// bill, not a missing field.
	ChargedCents    float64
	HasChargedCents bool
	// IsHeadless is informational only. Real cursor-agent Dashboard rows have
	// been observed with false, so reconciliation must not filter or identify
	// events by this flag.
	IsHeadless   bool
	IsChargeable bool
	// OccurrenceIndex disambiguates identical fingerprint rows inside one
	// fetched batch (Dashboard has no stable event id).
	OccurrenceIndex int
}

// fingerprint is the stable field hash without the occurrence index.
func (e UsageEvent) fingerprint() string {
	charged := "absent"
	if e.HasChargedCents {
		charged = strconv.FormatFloat(e.ChargedCents, 'f', -1, 64)
	}
	return fmt.Sprintf("%d|%s|%d|%d|%d|%d|%s",
		e.Timestamp.UTC().UnixMilli(),
		normalizeModel(e.Model),
		e.InputTokens,
		e.OutputTokens,
		e.CacheReadTokens,
		e.CacheWriteTokens,
		charged,
	)
}

// OccurrenceKey identifies one Dashboard event occurrence for claim tracking.
// The returned value is an opaque SHA-256 digest of the local fingerprint so
// servers never store reverse-engineerable event fields.
func (e UsageEvent) OccurrenceKey() string {
	raw := fmt.Sprintf("%s#%d", e.fingerprint(), e.OccurrenceIndex)
	return OpaqueClaimKey(raw)
}

// assignOccurrenceIndexes numbers identical fingerprints within a batch so
// duplicate rows remain distinct claim keys.
func assignOccurrenceIndexes(events []UsageEvent) []UsageEvent {
	if len(events) == 0 {
		return events
	}
	counts := make(map[string]int, len(events))
	out := make([]UsageEvent, len(events))
	for i, e := range events {
		fp := e.fingerprint()
		e.OccurrenceIndex = counts[fp]
		counts[fp]++
		out[i] = e
	}
	return out
}

// Client talks to Cursor Dashboard HTTP APIs using a local session cookie.
type Client struct {
	HTTPClient *http.Client
	BaseURL    string
}

func (c *Client) http() *http.Client {
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func (c *Client) base() string {
	if c != nil && strings.TrimSpace(c.BaseURL) != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return cursorAPIBase
}

func buildCursorHeaders(sessionToken string) http.Header {
	h := make(http.Header)
	h.Set("Accept", "application/json")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Cookie", "WorkosCursorSessionToken="+sessionToken)
	h.Set("Referer", "https://www.cursor.com/settings")
	h.Set("Origin", "https://cursor.com")
	h.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	return h
}

// filteredUsageRequest mirrors CodexBar: only page bounds + date window.
// Optional userId/teamId are omitted — the session cookie scopes the account.
type filteredUsageRequest struct {
	Page      int     `json:"page"`
	PageSize  int     `json:"pageSize"`
	StartDate *string `json:"startDate,omitempty"`
	EndDate   *string `json:"endDate,omitempty"`
}

type filteredUsageResponse struct {
	TotalUsageEventsCount int                     `json:"totalUsageEventsCount"`
	UsageEventsDisplay    []filteredUsageRawEvent `json:"usageEventsDisplay"`
}

type filteredUsageRawEvent struct {
	Timestamp    flexString    `json:"timestamp"`
	Model        string        `json:"model"`
	IsHeadless   bool          `json:"isHeadless"`
	IsChargeable bool          `json:"isChargeable"`
	ChargedCents optionalFloat `json:"chargedCents"`
	TokenUsage   struct {
		InputTokens      int64         `json:"inputTokens"`
		OutputTokens     int64         `json:"outputTokens"`
		CacheReadTokens  int64         `json:"cacheReadTokens"`
		CacheWriteTokens int64         `json:"cacheWriteTokens"`
		TotalCents       optionalFloat `json:"totalCents"`
	} `json:"tokenUsage"`
}

// FetchFilteredUsageEvents returns Dashboard usage events in [start, end].
// start/end are inclusive wall-clock bounds; the API takes epoch millis.
func (c *Client) FetchFilteredUsageEvents(ctx context.Context, sessionToken string, start, end time.Time) ([]UsageEvent, error) {
	if end.Before(start) {
		return nil, fmt.Errorf("end before start")
	}
	var out []UsageEvent
	page := 1
	for {
		batch, total, err := c.fetchFilteredUsagePage(ctx, sessionToken, start, end, page, defaultFilteredPageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(out) == total {
			break
		}
		if len(out) > total {
			return nil, fmt.Errorf("cursor filtered-usage-events returned %d events, expected %d", len(out), total)
		}
		if len(batch) == 0 || len(batch) < defaultFilteredPageSize {
			return nil, fmt.Errorf("cursor filtered-usage-events returned %d of %d events", len(out), total)
		}
		page++
		if page > 50 {
			return nil, fmt.Errorf("cursor filtered-usage-events exceeded 50 pages")
		}
	}
	return out, nil
}

func (c *Client) fetchFilteredUsagePage(ctx context.Context, sessionToken string, start, end time.Time, page, pageSize int) ([]UsageEvent, int, error) {
	startMS := strconv.FormatInt(start.UTC().UnixMilli(), 10)
	endMS := strconv.FormatInt(end.UTC().UnixMilli(), 10)
	payload := filteredUsageRequest{
		Page:      page,
		PageSize:  pageSize,
		StartDate: &startMS,
		EndDate:   &endMS,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+filteredUsageEventsPath, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header = buildCursorHeaders(sessionToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("cursor filtered-usage-events: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, 0, fmt.Errorf("cursor session expired or invalid")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("cursor filtered-usage-events status %d: %s", resp.StatusCode, truncateForErr(body))
	}
	var parsed filteredUsageResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, fmt.Errorf("parse filtered-usage-events: %w", err)
	}
	events := make([]UsageEvent, 0, len(parsed.UsageEventsDisplay))
	for _, rawEvt := range parsed.UsageEventsDisplay {
		ts, ok := rawEvt.Timestamp.Time()
		if !ok {
			continue
		}
		events = append(events, UsageEvent{
			Timestamp:        ts,
			Model:            strings.TrimSpace(rawEvt.Model),
			InputTokens:      rawEvt.TokenUsage.InputTokens,
			OutputTokens:     rawEvt.TokenUsage.OutputTokens,
			CacheReadTokens:  rawEvt.TokenUsage.CacheReadTokens,
			CacheWriteTokens: rawEvt.TokenUsage.CacheWriteTokens,
			ChargedCents:     rawEvt.ChargedCents.Value,
			HasChargedCents:  rawEvt.ChargedCents.Set,
			IsHeadless:       rawEvt.IsHeadless,
			IsChargeable:     rawEvt.IsChargeable,
		})
	}
	return events, parsed.TotalUsageEventsCount, nil
}

func truncateForErr(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// flexString accepts JSON string or number timestamps.
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*f = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*f = flexString(n.String())
	return nil
}

func (f flexString) Time() (time.Time, bool) {
	s := strings.TrimSpace(string(f))
	if s == "" {
		return time.Time{}, false
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		if ms < 1_000_000_000_000 {
			return time.Unix(ms, 0).UTC(), true
		}
		return time.UnixMilli(ms).UTC(), true
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}

// optionalFloat preserves JSON null/absent vs an explicit numeric zero.
type optionalFloat struct {
	Value float64
	Set   bool
}

func (f *optionalFloat) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*f = optionalFloat{}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			*f = optionalFloat{}
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*f = optionalFloat{Value: v, Set: true}
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = optionalFloat{Value: v, Set: true}
	return nil
}
