package handler

import "net/http"

// CasdoorLogin initiates the Casdoor OAuth flow by redirecting to the
// Casdoor authorization endpoint. Stub: returns 501 until the OAuth
// callback handler is implemented.
func (h *Handler) CasdoorLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"casdoor login not yet implemented"}`))
}

// CasdoorCallback handles the OAuth callback from Casdoor, exchanging
// the authorization code for tokens and provisioning the user session.
// Stub: returns 501 until implemented.
func (h *Handler) CasdoorCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`{"error":"casdoor callback not yet implemented"}`))
}
