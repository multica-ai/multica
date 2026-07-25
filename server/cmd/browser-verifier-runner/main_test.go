// CEREBRO-PATCH(browser-verifier-runner): Test the private verifier's fork-only configuration.
package main

// CEREBRO-PATCH(internal-agent-browser-qa): Support the private runner timeout regression test.
import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
)

func TestConfigRequiresSharedToken(t *testing.T) {
	t.Setenv("BROWSER_VERIFIER_TOKEN", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("runner started without an authentication token")
	}
}

// CEREBRO-PATCH(internal-agent-browser-qa): Lock the private runner's response-time ceiling.
func TestServerOutlivesTheLongestVerification(t *testing.T) {
	server := newBrowserVerifierServer(":0", http.NotFoundHandler())
	if server.WriteTimeout <= internalbrowserqa.MaxVerificationDuration {
		t.Fatalf(
			"write timeout %s must exceed verification ceiling %s",
			server.WriteTimeout,
			internalbrowserqa.MaxVerificationDuration,
		)
	}
}
