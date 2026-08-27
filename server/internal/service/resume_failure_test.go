package service

import "testing"

func TestResumeUnsafeFailureTreatsTraeInvalidParamAsPoisoned(t *testing.T) {
	errText := `traecli session/prompt failed: session/prompt: Internal error (code=-32603, data={"message":"stream disconnected before completion: We're sorry, the param is invalid. Please try with a valid param.","codex_error_info":"other"})`
	if !ResumeUnsafeFailure("agent_error.provider_network", errText) {
		t.Fatal("traecli invalid param failures must not resume the rejected session")
	}
}
