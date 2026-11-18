package handler

import (
	"testing"
)

func TestKagentAPIConfigurationBoundary(t *testing.T) {
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	if err := validateRemoteAgentConfig("kagent", []byte(`{"mode":"native-text"}`)); err == nil {
		t.Fatal("disabled provider accepted")
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	if err := validateRemoteAgentConfig("kagent", []byte(`{"mode":"native-text"}`)); err != nil {
		t.Fatal(err)
	}
	if err := validateRemoteAgentConfig("kagent", []byte(`{"mode":"native-text","token":"secret"}`)); err == nil {
		t.Fatal("untrusted connection configuration accepted")
	}
	if err := (qoderOverrides{model: "override"}).validateFor("kagent"); err == nil {
		t.Fatal("model override accepted")
	}
	if got := claimExecutionToken("kagent", "mat_secret"); got != "" {
		t.Fatal("task credential delivered to native-text provider")
	}
	if got := claimExecutionToken("claude", "mat_secret"); got != "mat_secret" {
		t.Fatal("existing provider credential changed")
	}
}
