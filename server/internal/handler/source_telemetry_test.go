package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/sourcebeacon"
)

// captureRecorder is a fake analytics.Client that records every captured
// event for assertions.
type captureRecorder struct{ events []analytics.Event }

func (c *captureRecorder) Capture(e analytics.Event) { c.events = append(c.events, e) }
func (c *captureRecorder) Close()                    {}

func postBeacon(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/self-host-source", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleSelfHostSourceBeacon(rec, req)
	return rec
}

func TestSelfHostSourceBeacon_ValidPayload(t *testing.T) {
	rec := &captureRecorder{}
	h := &Handler{Analytics: rec}
	uid := sourcebeacon.HashUID("salt", "user-1")
	inst := sourcebeacon.HashInstance("salt")

	resp := postBeacon(t, h, `{"v":1,"channels":["social_youtube","search"],"uid_hash":"`+uid+`","instance_hash":"`+inst+`"}`)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", resp.Code, resp.Body.String())
	}
	if len(rec.events) != 2 {
		t.Fatalf("captured %d events, want 2", len(rec.events))
	}
	for i, ch := range []string{"social_youtube", "search"} {
		e := rec.events[i]
		if e.Name != analytics.EventSelfHostSourceChannel {
			t.Errorf("event[%d].Name = %q", i, e.Name)
		}
		if e.DistinctID != "selfhost:"+uid {
			t.Errorf("event[%d].DistinctID = %q", i, e.DistinctID)
		}
		if !strings.Contains(e.DistinctID, ":") {
			t.Errorf("distinct_id must contain ':' to suppress user_id derivation")
		}
		if e.UUID != sourcebeacon.EventUUID(inst, uid, ch) {
			t.Errorf("event[%d].UUID = %q, want deterministic", i, e.UUID)
		}
		if e.Properties["source"] != ch {
			t.Errorf("event[%d].source = %v, want %q", i, e.Properties["source"], ch)
		}
		if e.Properties["deployment"] != "self_host" {
			t.Errorf("event[%d].deployment = %v", i, e.Properties["deployment"])
		}
		if e.Properties["instance_hash"] != inst {
			t.Errorf("event[%d].instance_hash = %v", i, e.Properties["instance_hash"])
		}
		if v, ok := e.Properties["$process_person_profile"].(bool); !ok || v {
			t.Errorf("event[%d] must set $process_person_profile=false, got %v", i, e.Properties["$process_person_profile"])
		}
	}
}

func TestSelfHostSourceBeacon_DropsUnknownChannelsKeepsValid(t *testing.T) {
	rec := &captureRecorder{}
	h := &Handler{Analytics: rec}
	uid := sourcebeacon.HashUID("salt", "u")
	inst := sourcebeacon.HashInstance("salt")

	resp := postBeacon(t, h, `{"v":1,"channels":["bogus","search"],"uid_hash":"`+uid+`","instance_hash":"`+inst+`"}`)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.Code)
	}
	if len(rec.events) != 1 || rec.events[0].Properties["source"] != "search" {
		t.Fatalf("expected only the valid channel captured, got %d events", len(rec.events))
	}
}

func TestSelfHostSourceBeacon_Rejections(t *testing.T) {
	uid := sourcebeacon.HashUID("salt", "u")
	inst := sourcebeacon.HashInstance("salt")
	cases := []struct {
		name string
		body string
	}{
		// The privacy red-line: any extra field (identity / source_other /
		// anything) is rejected, never logged or forwarded.
		{"unknown field", `{"v":1,"channels":["search"],"uid_hash":"` + uid + `","instance_hash":"` + inst + `","email":"a@b.com"}`},
		{"source_other field", `{"v":1,"channels":["other"],"uid_hash":"` + uid + `","instance_hash":"` + inst + `","source_other":"a podcast"}`},
		{"wrong version", `{"v":2,"channels":["search"],"uid_hash":"` + uid + `","instance_hash":"` + inst + `"}`},
		{"all invalid channels", `{"v":1,"channels":["nope"],"uid_hash":"` + uid + `","instance_hash":"` + inst + `"}`},
		{"empty channels", `{"v":1,"channels":[],"uid_hash":"` + uid + `","instance_hash":"` + inst + `"}`},
		{"invalid uid hash", `{"v":1,"channels":["search"],"uid_hash":"NOT-HEX","instance_hash":"` + inst + `"}`},
		{"malformed json", `{"v":1,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &captureRecorder{}
			h := &Handler{Analytics: rec}
			resp := postBeacon(t, h, tc.body)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", resp.Code, resp.Body.String())
			}
			if len(rec.events) != 0 {
				t.Fatalf("rejected payload must capture 0 events, got %d", len(rec.events))
			}
		})
	}
}

func TestSelfHostSourceBeacon_OversizedBody(t *testing.T) {
	rec := &captureRecorder{}
	h := &Handler{Analytics: rec}
	big := strings.Repeat("a", sourcebeacon.MaxBodyBytes+1)
	resp := postBeacon(t, h, `{"v":1,"channels":["search"],"uid_hash":"`+big+`","instance_hash":"x"}`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for oversized body", resp.Code)
	}
	if len(rec.events) != 0 {
		t.Fatal("oversized body must capture 0 events")
	}
}
