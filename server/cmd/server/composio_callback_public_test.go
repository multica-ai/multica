package main

import (
	"io"
	"net/http"
	"testing"
)

// TestComposioCallbackIsPublic_NoCookieNot401 locks in the MUL-3843 fix: the
// Composio OAuth callback must live OUTSIDE the Auth middleware group, because
// Composio 302-redirects the user's browser to it and the cookie session is
// frequently absent (expired session, SameSite=Strict / Safari ITP, private
// window, self-hosted callback subdomain). Before the fix the route sat under
// Auth, so a cookie-less browser got a hard 401 and a JSON blob instead of the
// settings redirect — the exact symptom Yushen hit.
//
// With no COMPOSIO_API_KEY configured in the test env, h.Composio == nil, so a
// cookie-less hit on the callback now reaches the handler and returns 503
// ("not configured") rather than being short-circuited to 401 by the Auth
// middleware. The precise non-401 code is incidental; what this test pins is
// that the request is NOT rejected by auth.
func TestComposioCallbackIsPublic_NoCookieNot401(t *testing.T) {
	// Deliberately send NO Authorization header / cookie — simulate the
	// cookie-stripped browser redirect coming back from Composio.
	resp, err := http.Get(testServer.URL + "/api/integrations/composio/callback?state=bogus&status=success&connected_account_id=ca_x")
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("callback returned 401 without a session — it is still behind the Auth group (regression of MUL-3843). body=%s", body)
	}
}

// TestComposioNonCallbackEndpointsStayGated is the other half of the invariant:
// moving the callback out of the Auth group must NOT loosen the four
// session-scoped endpoints. A cookie-less request to them must still 401.
func TestComposioNonCallbackEndpointsStayGated(t *testing.T) {
	gated := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/integrations/composio/connect/init"},
		{http.MethodGet, "/api/integrations/composio/toolkits"},
		{http.MethodGet, "/api/integrations/composio/connections"},
		{http.MethodDelete, "/api/integrations/composio/connections/11111111-1111-1111-1111-111111111111"},
	}
	for _, tc := range gated {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, testServer.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401 without a session, got %d — endpoint is no longer auth-gated", resp.StatusCode)
			}
		})
	}
}
