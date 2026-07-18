package main

import (
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The app advertises its capabilities on the cancel request (#5219). Browsers
// preflight a custom request header, so an entry missing from AllowedHeaders is
// not a degraded feature — it is a failed request: the cancel never reaches the
// server, and the user's prompt is lost in a way no server-side test can see.
func TestCORSAllowedHeaders_IncludeClientCapabilities(t *testing.T) {
	if !slices.Contains(corsAllowedHeaders, "X-Client-Capabilities") {
		t.Fatalf("X-Client-Capabilities missing from CORS allowed headers: %v", corsAllowedHeaders)
	}
	// Named so the constant and the header travel together: the capability is
	// useless if the header carrying it cannot cross the preflight.
	if protocol.AppCapabilityChatDraftRestoreV1 == "" {
		t.Fatal("AppCapabilityChatDraftRestoreV1 must be a non-empty capability token")
	}
}
