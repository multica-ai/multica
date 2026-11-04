package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Real response shape observed against the domestic Zhipu endpoint with a
// personal Coding Plan key (older plan: TIME_LIMIT monthly tool credits +
// TOKENS_LIMIT 5h window; HTTP 200 with code/success as the real health
// check).
const glmQuotaOldPlanBody = `{"code":200,"msg":"操作成功","data":{"limits":[` +
	`{"type":"TIME_LIMIT","unit":5,"number":1,"usage":4000,"currentValue":640,"remaining":3360,"percentage":16,` +
	`"nextResetTime":1790845772998,"usageDetails":[{"modelCode":"search-prime","usage":574}]},` +
	`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":1,"nextResetTime":1788808596818}` +
	`],"level":"max"},"success":true}`

// Newer plans answer with CREDIT_LIMIT pools instead; both must parse.
const glmQuotaCreditPlanBody = `{"code":200,"success":true,"data":{"limits":[` +
	`{"type":"CREDIT_LIMIT","usage":50,"currentValue":12.5,"remaining":37.5,"percentage":25,"nextResetTime":1790000000000},` +
	`{"type":"CREDIT_LIMIT","usage":500,"currentValue":80,"remaining":420,"percentage":16,"nextResetTime":1790100000000}` +
	`],"level":"pro"}}`

func TestParseGlmQuotaOldPlanShape(t *testing.T) {
	now := time.Unix(1788800000, 0)
	snap, err := parseGlmQuotaBody([]byte(glmQuotaOldPlanBody), now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if snap.Level != "max" {
		t.Fatalf("level = %q, want max", snap.Level)
	}
	if snap.ObservedAt != now.Unix() {
		t.Fatalf("observedAt = %d, want %d", snap.ObservedAt, now.Unix())
	}
	if len(snap.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(snap.Windows))
	}
	tl := snap.Windows[0]
	if tl.Type != "TIME_LIMIT" || tl.Usage == nil || *tl.Usage != 4000 ||
		tl.CurrentValue == nil || *tl.CurrentValue != 640 || tl.Remaining == nil || *tl.Remaining != 3360 {
		t.Fatalf("TIME_LIMIT window malformed: %+v", tl)
	}
	if tl.UsedPercent == nil || *tl.UsedPercent != 16 {
		t.Fatalf("used percent = %+v, want 16", tl.UsedPercent)
	}
	if tl.ResetsAt == nil || *tl.ResetsAt != 1790845772 {
		t.Fatalf("reset = %+v, want 1790845772 (ms→s)", tl.ResetsAt)
	}
	tok := snap.Windows[1]
	if tok.Type != "TOKENS_LIMIT" || tok.Usage != nil || tok.UsedPercent == nil || *tok.UsedPercent != 1 {
		t.Fatalf("TOKENS_LIMIT window malformed: %+v", tok)
	}
}

func TestParseGlmQuotaCreditPlanShape(t *testing.T) {
	snap, err := parseGlmQuotaBody([]byte(glmQuotaCreditPlanBody), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(snap.Windows) != 2 || snap.Windows[0].Type != "CREDIT_LIMIT" {
		t.Fatalf("credit windows malformed: %+v", snap.Windows)
	}
	if snap.Windows[0].Remaining == nil || *snap.Windows[0].Remaining != 37.5 {
		t.Fatalf("remaining = %+v, want 37.5", snap.Windows[0].Remaining)
	}
}

// The endpoint answers HTTP 200 for business failures; those must surface
// as errors, not as an empty-but-successful snapshot.
func TestParseGlmQuotaBusinessFailure(t *testing.T) {
	for name, body := range map[string]string{
		"code":    `{"code":401,"msg":"unauthorized","success":false}`,
		"success": `{"code":200,"msg":"boom","success":false}`,
		"empty":   `{"code":200,"success":true,"data":{"limits":[]}}`,
		"garbage": `not json at all`,
	} {
		if _, err := parseGlmQuotaBody([]byte(body), time.Now()); err == nil {
			t.Fatalf("%s: expected error, got snapshot", name)
		}
	}
}

func TestGlmQuotaMonitorPollKeepsPreviousSnapshotOnError(t *testing.T) {
	var fail bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-key" {
			w.WriteHeader(401)
			return
		}
		if fail {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"code":500,"success":false}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(glmQuotaOldPlanBody))
	}))
	defer srv.Close()

	m := &GlmQuotaMonitor{
		apiKey:   "test-key",
		baseURL:  srv.URL,
		interval: time.Hour,
		client:   srv.Client(),
	}
	m.poll(context.Background())
	st := m.Status()
	if !st.Enabled || st.Quota == nil || len(st.Quota.Windows) != 2 || st.LastError != "" {
		t.Fatalf("first poll: %+v", st)
	}
	first := st.Quota.ObservedAt

	fail = true
	m.poll(context.Background())
	st = m.Status()
	if st.Quota == nil || st.Quota.ObservedAt != first {
		t.Fatalf("failed round must keep the previous snapshot, got %+v", st)
	}
	if st.LastError == "" {
		t.Fatal("failed round must record last_error")
	}
}

func TestGlmQuotaMonitorStaleAging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(glmQuotaCreditPlanBody))
	}))
	defer srv.Close()

	m := &GlmQuotaMonitor{apiKey: "k", baseURL: srv.URL, interval: time.Hour, client: srv.Client()}
	m.poll(context.Background())
	if m.Status().Stale {
		t.Fatal("fresh snapshot must not be stale")
	}
	// Age the snapshot past glmQuotaStaleAfter without sleeping: rewrite
	// observedAt under the lock via a direct fetch pinned to the past is not
	// exposed, so simulate by constructing the monitor state manually.
	m.mu.Lock()
	old := m.snapshot.ObservedAt - int64(glmQuotaStaleAfter.Seconds()) - 3600
	m.snapshot.ObservedAt = old
	m.mu.Unlock()
	if !m.Status().Stale {
		t.Fatal("aged snapshot must be stale")
	}
}

func TestGlmQuotaStatusAnchorPassthrough(t *testing.T) {
	m := &GlmQuotaMonitor{apiKey: "k", anchorDevice: "agent-main-01"}
	if got := m.Status().AnchorDevice; got != "agent-main-01" {
		t.Fatalf("anchor = %q, want agent-main-01", got)
	}
}

func TestGlmQuotaStatusJSONShape(t *testing.T) {
	b, err := json.Marshal(GlmQuotaStatus{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"enabled":false}` {
		t.Fatalf("disabled payload = %s", b)
	}
}

// Newer plans return several TOKENS_LIMIT/CREDIT_LIMIT windows whose real
// period is only in unit/number (OL-1205): 3+5 → 5h, 6 → weekly regardless
// of number (plans disagree: 1 and 7 both mean one week), 5 → monthly with
// no minute count. The type must never imply the period.
func TestParseGlmQuotaWindowPeriods(t *testing.T) {
	body := `{"code":200,"success":true,"data":{"limits":[` +
		`{"type":"TIME_LIMIT","unit":5,"number":1,"percentage":16,"nextResetTime":1790845772998},` +
		`{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":1,"nextResetTime":1788808596818},` +
		`{"type":"TOKENS_LIMIT","unit":6,"number":7,"percentage":18,"nextResetTime":1790000000000},` +
		`{"type":"CREDIT_LIMIT","unit":6,"number":1,"percentage":40,"nextResetTime":1790100000000},` +
		`{"type":"CREDIT_LIMIT","percentage":25}` +
		`],"level":"max"}}`
	snap, err := parseGlmQuotaBody([]byte(body), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []int{0, 300, 10080, 10080, 0}
	if len(snap.Windows) != len(want) {
		t.Fatalf("windows = %d, want %d", len(snap.Windows), len(want))
	}
	for i, w := range snap.Windows {
		if w.WindowMinutes != want[i] {
			t.Errorf("window %d (%s unit-period) = %d minutes, want %d", i, w.Type, w.WindowMinutes, want[i])
		}
	}
}

func TestGlmQuotaWindowMinutes(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name         string
		unit, number *float64
		want         int
	}{
		{"nil unit", nil, f(5), 0},
		{"hours x number", f(3), f(5), 300},
		{"hours without number", f(3), nil, 0},
		{"week number=7", f(6), f(7), 10080},
		{"week number=1", f(6), f(1), 10080},
		{"week without number", f(6), nil, 10080},
		{"month", f(5), f(1), 0},
		{"unknown unit", f(9), f(2), 0},
	}
	for _, c := range cases {
		if got := glmQuotaWindowMinutes(c.unit, c.number); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
