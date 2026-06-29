package sourcebeacon

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestShouldSend(t *testing.T) {
	cases := []struct {
		name string
		in   ShouldSendInput
		want bool
	}{
		{"production self-host", ShouldSendInput{Environment: "production", AppHost: "acme.example.com"}, true},
		{"analytics disabled", ShouldSendInput{AnalyticsDisabled: true, Environment: "production", AppHost: "acme.example.com"}, false},
		{"not production", ShouldSendInput{Environment: "staging", AppHost: "acme.example.com"}, false},
		{"dev", ShouldSendInput{Environment: "dev", AppHost: "acme.example.com"}, false},
		{"localhost", ShouldSendInput{Environment: "production", AppHost: "localhost"}, false},
		{"empty host", ShouldSendInput{Environment: "production", AppHost: ""}, false},
		{"official multica.ai", ShouldSendInput{Environment: "production", AppHost: "multica.ai"}, false},
		{"official app subdomain", ShouldSendInput{Environment: "production", AppHost: "app.multica.ai"}, false},
		{"official staging subdomain", ShouldSendInput{Environment: "production", AppHost: "staging.multica.ai"}, false},
		{"official api subdomain", ShouldSendInput{Environment: "production", AppHost: "api.multica.ai"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldSend(tc.in); got != tc.want {
				t.Fatalf("ShouldSend(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalHost(t *testing.T) {
	cases := map[string]string{
		"https://acme.example.com":      "acme.example.com",
		"http://localhost:3000":         "localhost",
		"acme.example.com":              "acme.example.com",
		"https://APP.MULTICA.AI/path":   "app.multica.ai",
		"":                              "",
		"https://acme.example.com:8443": "acme.example.com",
	}
	for raw, want := range cases {
		if got := canonicalHost(raw); got != want {
			t.Errorf("canonicalHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestHashingIsDeterministicAndSalted(t *testing.T) {
	uid1 := HashUID("salt-a", "user-1")
	if uid1 != HashUID("salt-a", "user-1") {
		t.Fatal("HashUID not deterministic")
	}
	if len(uid1) != 32 {
		t.Fatalf("uid hash length = %d, want 32", len(uid1))
	}
	// Different salt (different instance) → different hash for same user.
	if uid1 == HashUID("salt-b", "user-1") {
		t.Fatal("same user across different salts must hash differently")
	}
	// Different user, same salt → different hash.
	if uid1 == HashUID("salt-a", "user-2") {
		t.Fatal("different users must hash differently")
	}
	// instance hash is stable and distinct from a uid hash.
	if HashInstance("salt-a") != HashInstance("salt-a") {
		t.Fatal("HashInstance not deterministic")
	}
	if HashInstance("salt-a") == uid1 {
		t.Fatal("instance hash must differ from uid hash")
	}
}

func TestEventUUIDDeterministicPerChannel(t *testing.T) {
	a := EventUUID("inst", "uid", "social_youtube")
	if a != EventUUID("inst", "uid", "social_youtube") {
		t.Fatal("EventUUID not deterministic")
	}
	if a == EventUUID("inst", "uid", "search") {
		t.Fatal("different channels must yield different event uuids")
	}
	if a == EventUUID("inst2", "uid", "social_youtube") {
		t.Fatal("different instances must yield different event uuids")
	}
	if len(a) != 36 { // canonical UUID string
		t.Fatalf("event uuid not canonical: %q", a)
	}
}

func TestFilterValidChannels(t *testing.T) {
	got := FilterValidChannels([]string{"social_youtube", "bogus", "search", "social_youtube", "", "other"})
	want := []string{"social_youtube", "search", "other"}
	if len(got) != len(want) {
		t.Fatalf("FilterValidChannels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("FilterValidChannels = %v, want %v", got, want)
		}
	}
	if len(FilterValidChannels([]string{"nope", "also_nope"})) != 0 {
		t.Fatal("all-invalid input must yield empty slice")
	}
}

func TestIsValidHash(t *testing.T) {
	if !IsValidHash(HashUID("s", "u")) {
		t.Fatal("a real hash must validate")
	}
	for _, bad := range []string{"", "xyz", "SHORT", "ABCDEF0123456789", "g123456789012345"} {
		if IsValidHash(bad) {
			t.Errorf("IsValidHash(%q) = true, want false", bad)
		}
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSenderEnabledGating(t *testing.T) {
	if NewSender(SenderConfig{Enabled: false, Salt: "s"}).Enabled() {
		t.Fatal("Enabled:false must be disabled")
	}
	if NewSender(SenderConfig{Enabled: true, Salt: ""}).Enabled() {
		t.Fatal("empty salt must be disabled")
	}
	if !NewSender(SenderConfig{Enabled: true, Salt: "s"}).Enabled() {
		t.Fatal("enabled + salt must be enabled")
	}
	var nilSender *Sender
	if nilSender.Enabled() {
		t.Fatal("nil sender must be disabled")
	}
}

func TestSenderMaybeSendPostsPayload(t *testing.T) {
	got := make(chan Payload, 1)
	gotURL := make(chan string, 1)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var p Payload
		_ = json.Unmarshal(body, &p)
		got <- p
		gotURL <- r.URL.String()
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})}
	s := NewSender(SenderConfig{Enabled: true, Salt: "salt-x", UpstreamURL: "https://ingest.example", HTTPClient: client})

	s.MaybeSend("user-1", []string{"social_youtube", "bogus", "search"})

	select {
	case p := <-got:
		if p.V != SchemaVersion {
			t.Errorf("v = %d, want %d", p.V, SchemaVersion)
		}
		wantChannels := []string{"social_youtube", "search"}
		if len(p.Channels) != len(wantChannels) {
			t.Fatalf("channels = %v, want %v", p.Channels, wantChannels)
		}
		if p.UIDHash != HashUID("salt-x", "user-1") || p.InstanceHash != HashInstance("salt-x") {
			t.Error("payload hashes do not match expected salt hashing")
		}
		if u := <-gotURL; u != "https://ingest.example/api/telemetry/self-host-source" {
			t.Errorf("url = %q", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected a beacon POST, got none")
	}
}

func TestSenderMaybeSendNoOpCases(t *testing.T) {
	calls := make(chan struct{}, 1)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls <- struct{}{}
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
	})}
	s := NewSender(SenderConfig{Enabled: true, Salt: "salt-x", HTTPClient: client})

	s.MaybeSend("user-1", []string{"bogus"}) // no valid channel
	s.MaybeSend("", []string{"search"})      // no user id

	select {
	case <-calls:
		t.Fatal("no beacon should have been sent")
	case <-time.After(150 * time.Millisecond):
	}
}
