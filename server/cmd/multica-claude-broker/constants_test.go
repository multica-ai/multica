package main

import "testing"

func TestConstants_Embedded(t *testing.T) {
	if Constants.Endpoint != "https://platform.claude.com/v1/oauth/token" {
		t.Errorf("endpoint = %q", Constants.Endpoint)
	}
	if Constants.ClientID == "" {
		t.Errorf("client_id empty")
	}
	if Constants.VersionHeader == "" {
		t.Errorf("version_header empty")
	}
	if Constants.Scopes == "" {
		t.Errorf("scopes empty")
	}
	if Constants.ClaudeVersion == "" {
		t.Errorf("claude_version (from _meta) empty")
	}
}
