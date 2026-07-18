// CEREBRO-PATCH(browser-verifier-runner): Test the private verifier's fork-only configuration.
package main

import "testing"

func TestConfigRequiresSharedToken(t *testing.T) {
	t.Setenv("BROWSER_VERIFIER_TOKEN", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("runner started without an authentication token")
	}
}
