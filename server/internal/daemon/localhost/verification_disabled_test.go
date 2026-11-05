//go:build verificationprodtest && !verificationdev

package localhost

import (
	"net/http"
	"strings"
	"testing"
)

func TestVerificationRoutesAbsentFromProduction(t *testing.T) {
	t.Setenv("MULTICA_VERIFICATION_MODE", "1")
	t.Setenv("MULTICA_VERIFICATION_TOKEN", strings.Repeat("a", 64))
	server, _ := newV2Server(t)
	for _, path := range []string{"capabilities", "events", "fixture", "ws"} {
		status, _ := v2Do(t, server, http.MethodGet, "/local/v2/verification/"+path, nil, v2Headers("user-1"))
		if status != http.StatusNotFound {
			t.Fatalf("production route %s: %d", path, status)
		}
	}
	status, _ := v2Do(t, server, http.MethodPost, "/local/v2/verification/import", map[string]any{"schemaVersion": 1}, v2Headers("user-1"))
	if status != http.StatusNotFound {
		t.Fatalf("production import route: %d", status)
	}
}
