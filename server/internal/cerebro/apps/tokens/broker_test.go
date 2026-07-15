package tokens

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testMemberID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	testAppID    = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	testRunID    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

func TestBrokerIssuesAppBoundPersonalKeyAndCachesIt(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.URL.Path; got != "/api/registry/v1/sessions/exchange" {
			t.Errorf("path=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer rk_system" {
			t.Errorf("authorization=%q", got)
		}
		var body struct {
			Principal string `json:"principal"`
			ViaApp    struct {
				ID      string  `json:"id"`
				Version string  `json:"version"`
				RunID   string  `json:"run_id"`
				Scopes  []Scope `json:"scopes"`
			} `json:"via_app"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Principal != "member:"+testMemberID || body.ViaApp.ID != testAppID || body.ViaApp.Version != "1.2.3" || body.ViaApp.RunID != testRunID {
			t.Errorf("unexpected exchange body: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":        "sk_personal",
			"session":    map[string]any{"id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"},
			"expires_at": now.Add(time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	b := NewBroker(Config{BaseURL: server.URL, SystemKey: "rk_system", TTLSeconds: 3600}, server.Client())
	b.now = func() time.Time { return now }
	identity := Identity{
		MemberID: testMemberID,
		App:      AppGrant{ID: testAppID, Version: "1.2.3", RunID: testRunID, Scopes: []Scope{{ResourceType: "data_source", ResourceID: "products", Access: "read"}}},
	}

	first, err := b.PersonalKey(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.PersonalKey(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key != "sk_personal" || first.SessionID == "" || second.Key != first.Key {
		t.Fatalf("unexpected tokens: first=%+v second=%+v", first, second)
	}
	if first.RunID != testRunID {
		t.Fatalf("token must expose its audit run id; got %q", first.RunID)
	}
	// The app calls whichever gateway belongs to the registry the key came from,
	// so it never has to hard code an environment of its own.
	if first.AIBaseURL != server.URL+"/api/ai/proxy/v1" {
		t.Fatalf("token must carry the gateway base URL for this registry; got %q", first.AIBaseURL)
	}
	if calls.Load() != 1 {
		t.Fatalf("cached key should avoid another exchange; calls=%d", calls.Load())
	}
	if b.Forget(identity) != 1 {
		t.Fatal("Forget should remove the cached app key")
	}
	if _, err := b.PersonalKey(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("forgotten key must be exchanged again; calls=%d", calls.Load())
	}
}

func TestBrokerRefreshesInsideExpirySkew(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":        "sk_key_" + string(rune('0'+call)),
			"session":    map[string]any{"id": "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"},
			"expires_at": now.Add(30 * time.Second).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	b := NewBroker(Config{BaseURL: server.URL, SystemKey: "rk_system"}, server.Client())
	b.now = func() time.Time { return now }
	identity := Identity{MemberID: testMemberID, App: AppGrant{ID: testAppID, Version: "1.0.0", RunID: testRunID, Scopes: []Scope{{ResourceType: "app", ResourceID: testAppID, Access: "read"}}}}
	if _, err := b.PersonalKey(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PersonalKey(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("near-expiry key must refresh; calls=%d", calls.Load())
	}
}

func TestBrokerFailsClosedForMissingIdentityScopeOrConfig(t *testing.T) {
	valid := Identity{MemberID: testMemberID, App: AppGrant{ID: testAppID, Version: "1.0.0", RunID: testRunID, Scopes: []Scope{{ResourceType: "app", ResourceID: testAppID, Access: "read"}}}}
	tests := []struct {
		name     string
		config   Config
		identity Identity
	}{
		{name: "system key", config: Config{BaseURL: "https://registry.example"}, identity: valid},
		{name: "member", config: Config{BaseURL: "https://registry.example", SystemKey: "rk"}, identity: Identity{App: valid.App}},
		{name: "run", config: Config{BaseURL: "https://registry.example", SystemKey: "rk"}, identity: Identity{MemberID: testMemberID, App: AppGrant{ID: testAppID, Version: "1.0.0", Scopes: valid.App.Scopes}}},
		{name: "scopes", config: Config{BaseURL: "https://registry.example", SystemKey: "rk"}, identity: Identity{MemberID: testMemberID, App: AppGrant{ID: testAppID, Version: "1.0.0"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewBroker(tc.config, nil).PersonalKey(context.Background(), tc.identity); err == nil {
				t.Fatal("expected fail-closed validation error")
			}
		})
	}
}

func TestParseWorkflowIdentityEnvelope(t *testing.T) {
	raw := []byte(`{"principal":{"type":"member","id":"` + testMemberID + `"},"app":{"id":"` + testAppID + `","version":"1.0.0","scopes":[{"resource_type":"data_source","resource_id":"products","access":"read_write"}]}}`)
	identity, err := ParseWorkflowIdentityEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if identity.MemberID != testMemberID || identity.App.ID != testAppID || len(identity.App.Scopes) != 1 {
		t.Fatalf("identity=%+v", identity)
	}
}
