package claudemanagedruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// eventLog fakes GET /sessions/sess_1/events: ascending processed_at order,
// an opaque page cursor, and created_at[gte] compared against processed_at.
// A page cursor is only valid with the filters of the request that issued it.
type eventLog struct {
	t        *testing.T
	mu       sync.Mutex
	events   []map[string]any
	pageSize int
	requests []url.Values
	issued   map[string]string
}

// filters encodes every query parameter except the page cursor.
func filters(q url.Values) string {
	rest := url.Values{}
	for k, v := range q {
		if k != "page" {
			rest[k] = v
		}
	}
	return rest.Encode()
}

func (l *eventLog) add(id, typ, at string, fields map[string]any) {
	e := map[string]any{"id": id, "type": typ, "processed_at": at}
	for k, v := range fields {
		e[k] = v
	}
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

func (l *eventLog) message(id, at, text string) {
	l.add(id, "agent.message", at, map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
}

func (l *eventLog) idle(id, at, reason string) {
	l.add(id, "session.status_idle", at, map[string]any{"stop_reason": map[string]string{"type": reason}})
}

func (l *eventLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/sessions/sess_1/events" || r.Method != http.MethodGet {
		l.t.Errorf("unexpected Claude request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	l.requests = append(l.requests, q)
	if q.Get("order") != "asc" || q.Has("after_id") {
		l.t.Errorf("unsupported event query %s", r.URL.RawQuery)
	}
	start := 0
	if p := q.Get("page"); p != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(p, "page_"))
		if err != nil {
			l.t.Errorf("unknown page cursor %q", p)
		}
		if want, ok := l.issued[p]; !ok || filters(q) != want {
			l.t.Errorf("page %s requested with filters %q, issued with %q", p, filters(q), want)
		}
		start = n
	} else if gte := q.Get("created_at[gte]"); gte != "" {
		start = len(l.events)
		for i, e := range l.events {
			if e["processed_at"].(string) >= gte {
				start = i
				break
			}
		}
	}
	end := min(start+l.pageSize, len(l.events))
	body := map[string]any{"data": l.events[start:end], "next_page": nil}
	if end < len(l.events) {
		next := fmt.Sprintf("page_%d", end)
		if l.issued == nil {
			l.issued = map[string]string{}
		}
		l.issued[next] = filters(q)
		body["next_page"] = next
	}
	json.NewEncoder(w).Encode(body)
}

// startOnLog starts the run against the harness, then points the bridge at
// a fake event log seeded with the submitted user message.
func startOnLog(t *testing.T, pageSize int) (*harness, *eventLog) {
	t.Helper()
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	l := &eventLog{t: t, pageSize: pageSize}
	l.add("evt_user", "user.message", "2026-01-01T00:00:00Z", map[string]any{"content": []map[string]string{{"type": "text", "text": h.prompt}}})
	s := httptest.NewServer(l)
	t.Cleanup(s.Close)
	h.b.anthropic.base = s.URL
	return h, l
}

// restart reloads the bridge state from the on-disk journal.
func (h *harness) restart(t *testing.T) {
	t.Helper()
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
}

func (h *harness) texts() []string {
	var out []string
	for _, m := range h.messages {
		out = append(out, fmt.Sprint(m["content"]))
	}
	return out
}

func TestEventsPageForwardThroughHistory(t *testing.T) {
	h, l := startOnLog(t, 2)
	for i := 1; i <= 4; i++ {
		l.message(fmt.Sprintf("evt_%d", i), fmt.Sprintf("2026-01-01T00:00:0%dZ", i), fmt.Sprintf("m%d", i))
	}
	l.idle("evt_idle", "2026-01-01T00:00:05Z", "end_turn")
	for range 5 {
		if h.complete != nil {
			break
		}
		if err := h.b.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		h.restart(t)
	}
	if h.complete == nil {
		t.Fatal("run did not complete")
	}
	if got := strings.Join(h.texts(), ","); got != "m1,m2,m3,m4" {
		t.Fatalf("messages = %s", got)
	}
	if l.requests[1].Get("page") != "page_2" || l.requests[2].Get("page") != "page_4" {
		t.Fatalf("page cursor not followed: %v", l.requests)
	}
}

// failMessage makes the Multica side reject the first task message whose
// content is text, then forwards everything else to the harness.
func (h *harness) failMessage(t *testing.T, text string) {
	t.Helper()
	target, err := url.Parse(h.b.multica.base)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	failed := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !failed && strings.HasSuffix(r.URL.Path, "/messages") {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"content":"`+text+`"`) {
				failed = true
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	h.b.multica.base = s.URL
}

func TestEventsResumeMidPageAfterRestart(t *testing.T) {
	h, l := startOnLog(t, 100)
	l.message("evt_1", "2026-01-01T00:00:01Z", "m1")
	l.message("evt_2", "2026-01-01T00:00:02Z", "m2")
	l.message("evt_3", "2026-01-01T00:00:03Z", "m3")
	l.idle("evt_idle", "2026-01-01T00:00:04Z", "end_turn")
	h.failMessage(t, "m3")
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("expected message failure")
	}
	h.restart(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.texts(), ","); got != "m1,m2,m3" || h.complete == nil {
		t.Fatalf("messages = %s, complete = %v", got, h.complete)
	}
	if got := l.requests[len(l.requests)-1].Get("created_at[gte]"); got != "2026-01-01T00:00:02Z" {
		t.Fatalf("resume bound = %q", got)
	}
}

func TestEventsResumeAfterFinalPage(t *testing.T) {
	h, l := startOnLog(t, 2)
	l.message("evt_1", "2026-01-01T00:00:01Z", "m1")
	l.message("evt_2", "2026-01-01T00:00:02Z", "m2")
	for range 2 {
		if err := h.b.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		h.restart(t)
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	resumed := len(l.requests)
	l.message("evt_3", "2026-01-01T00:00:03Z", "m3")
	l.idle("evt_idle", "2026-01-01T00:00:04Z", "end_turn")
	for range 2 {
		if err := h.b.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		h.restart(t)
	}
	if got := strings.Join(h.texts(), ","); got != "m1,m2,m3" || h.complete == nil {
		t.Fatalf("messages = %s, complete = %v", got, h.complete)
	}
	if q := l.requests[resumed]; q.Get("page") != "" || q.Get("created_at[gte]") != "2026-01-01T00:00:02Z" {
		t.Fatalf("resume query = %v", q)
	}
}

func TestEventsResumeScanKeepsFiltersWithPageCursor(t *testing.T) {
	h, l := startOnLog(t, 2)
	for i := 1; i <= 4; i++ {
		l.message(fmt.Sprintf("evt_%d", i), "2026-01-01T00:00:01Z", fmt.Sprintf("m%d", i))
	}
	l.idle("evt_idle", "2026-01-01T00:00:02Z", "end_turn")
	r := h.b.state.first()
	r.Cursor, r.CursorAt, r.Page = "evt_3", "2026-01-01T00:00:01Z", ""
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.texts(), ","); got != "m4" || h.complete == nil {
		t.Fatalf("messages = %s, complete = %v", got, h.complete)
	}
	for i, q := range l.requests {
		if q.Get("created_at[gte]") != "2026-01-01T00:00:01Z" || q.Get("limit") != "100" {
			t.Errorf("request %d dropped the resume filters: %v", i, q)
		}
	}
	if len(l.requests) != 3 || l.requests[2].Get("page") != "page_5" {
		t.Fatalf("requests = %v", l.requests)
	}
}

func TestEventsSharingTimestampAreEmittedOnce(t *testing.T) {
	h, l := startOnLog(t, 100)
	l.message("evt_1", "2026-01-01T00:00:01Z", "m1")
	l.message("evt_2", "2026-01-01T00:00:01Z", "m2")
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	l.message("evt_3", "2026-01-01T00:00:01Z", "m3")
	l.idle("evt_idle", "2026-01-01T00:00:02Z", "end_turn")
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.texts(), ","); got != "m1,m2,m3" || h.complete == nil {
		t.Fatalf("messages = %s, complete = %v", got, h.complete)
	}
	keys := map[any]bool{}
	for _, m := range h.messages {
		if keys[m["idempotency_key"]] {
			t.Fatalf("duplicate idempotency key %v", m["idempotency_key"])
		}
		keys[m["idempotency_key"]] = true
	}
	if !keys["claude:evt_2"] {
		t.Fatalf("idempotency keys = %v", keys)
	}
}

func TestEventsTerminalMapping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		events  func(*eventLog)
		outcome string
		failure string
	}{
		{"end_turn", func(l *eventLog) { l.idle("evt_end", "2026-01-01T00:00:02Z", "end_turn") }, "completed", ""},
		{"requires_action", func(l *eventLog) { l.idle("evt_end", "2026-01-01T00:00:02Z", "requires_action") }, "failed", "requires_action"},
		{"retries_exhausted", func(l *eventLog) { l.idle("evt_end", "2026-01-01T00:00:02Z", "retries_exhausted") }, "failed", "retries_exhausted"},
		{"budget_reached", func(l *eventLog) { l.idle("evt_end", "2026-01-01T00:00:02Z", "budget_reached") }, "failed", "budget_reached"},
		{"refusal", func(l *eventLog) { l.idle("evt_end", "2026-01-01T00:00:02Z", "refusal") }, "failed", "refusal"},
		{"terminated", func(l *eventLog) { l.add("evt_end", "session.status_terminated", "2026-01-01T00:00:02Z", nil) }, "failed", "terminated"},
		{"deleted", func(l *eventLog) { l.add("evt_end", "session.deleted", "2026-01-01T00:00:02Z", nil) }, "failed", "terminated"},
		{"rescheduled then end_turn", func(l *eventLog) {
			l.add("evt_resched", "session.status_rescheduled", "2026-01-01T00:00:02Z", nil)
			l.idle("evt_end", "2026-01-01T00:00:03Z", "end_turn")
		}, "completed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, l := startOnLog(t, 100)
			l.message("evt_reply", "2026-01-01T00:00:01Z", "done")
			tc.events(l)
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			switch tc.outcome {
			case "completed":
				if h.complete == nil || h.failure != nil {
					t.Fatalf("complete = %v, failure = %v", h.complete, h.failure)
				}
			case "failed":
				if h.complete != nil || !strings.Contains(fmt.Sprint(h.failure["error"]), tc.failure) {
					t.Fatalf("complete = %v, failure = %v", h.complete, h.failure)
				}
			}
		})
	}
}

func TestEventsRescheduledIsNotTerminal(t *testing.T) {
	h, l := startOnLog(t, 100)
	l.add("evt_resched", "session.status_rescheduled", "2026-01-01T00:00:01Z", nil)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete != nil || h.failure != nil || h.b.state.first() == nil {
		t.Fatalf("rescheduled ended the run: complete = %v, failure = %v", h.complete, h.failure)
	}
}
