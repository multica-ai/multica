package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func decodePlanLimits(t *testing.T, body []byte) planLimitsResponse {
	t.Helper()
	var out planLimitsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
	return out
}

func TestGetAgentPlanLimits_NoBrokerConfigured(t *testing.T) {
	h := &Handler{cfg: Config{ClaudeBrokerURL: ""}}
	rec := httptest.NewRecorder()
	h.GetAgentPlanLimits(rec, httptest.NewRequest(http.MethodGet, "/api/agent-usage/plan-limits", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decodePlanLimits(t, rec.Body.Bytes()); got.Available {
		t.Errorf("available = true, want false when no broker configured")
	}
}

func TestGetAgentPlanLimits_ProxiesSnapshot(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" {
			t.Errorf("broker path = %q, want /usage", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"five_hour": {"utilization": 42.0, "resets_at": "2026-06-12T20:00:00Z"},
			"seven_day": {"utilization": 17.0, "resets_at": "2026-06-18T00:00:00Z"},
			"seven_day_opus": null,
			"seven_day_sonnet": {"utilization": 3.0, "resets_at": "2026-06-17T00:00:00Z"},
			"fetched_at": "2026-06-12T16:50:00Z"
		}`))
	}))
	defer broker.Close()

	h := &Handler{cfg: Config{ClaudeBrokerURL: broker.URL}}
	rec := httptest.NewRecorder()
	h.GetAgentPlanLimits(rec, httptest.NewRequest(http.MethodGet, "/api/agent-usage/plan-limits", nil))

	got := decodePlanLimits(t, rec.Body.Bytes())
	if !got.Available {
		t.Fatal("available = false, want true")
	}
	if got.FiveHour == nil || got.FiveHour.Utilization != 42.0 {
		t.Errorf("five_hour = %+v", got.FiveHour)
	}
	if got.SevenDay == nil || got.SevenDay.Utilization != 17.0 {
		t.Errorf("seven_day = %+v", got.SevenDay)
	}
	if got.SevenDayOpus != nil {
		t.Errorf("seven_day_opus = %+v, want nil", got.SevenDayOpus)
	}
}

func TestGetAgentPlanLimits_BrokerDownDegrades(t *testing.T) {
	// Point at a closed server so the dial fails.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	h := &Handler{cfg: Config{ClaudeBrokerURL: url}}
	rec := httptest.NewRecorder()
	h.GetAgentPlanLimits(rec, httptest.NewRequest(http.MethodGet, "/api/agent-usage/plan-limits", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when broker down", rec.Code)
	}
	if got := decodePlanLimits(t, rec.Body.Bytes()); got.Available {
		t.Errorf("available = true, want false when broker unreachable")
	}
}

func TestGetAgentPlanLimits_MalformedBrokerBodyDegrades(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer broker.Close()

	h := &Handler{cfg: Config{ClaudeBrokerURL: broker.URL}}
	rec := httptest.NewRecorder()
	h.GetAgentPlanLimits(rec, httptest.NewRequest(http.MethodGet, "/api/agent-usage/plan-limits", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := decodePlanLimits(t, rec.Body.Bytes()); got.Available {
		t.Errorf("available = true, want false on malformed broker body")
	}
}
