package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestParseResponseEngineStartupConfigDisabledRequiresNoEndpoint(t *testing.T) {
	cfg, err := parseResponseEngineStartupConfig("", "", "", "")
	if err != nil {
		t.Fatalf("parse disabled config: %v", err)
	}
	if cfg.Mode != service.ResponseEngineLegacy {
		t.Fatalf("mode=%q, want LEGACY", cfg.Mode)
	}
	if cfg.Finalizer != nil {
		t.Fatal("disabled response engine unexpectedly constructed finalizer")
	}
}

func TestParseResponseEngineStartupConfigObserveBuildsFinalizer(t *testing.T) {
	cfg, err := parseResponseEngineStartupConfig(
		"true",
		"false",
		"http://response-engine.internal:8080",
		"service-token",
	)
	if err != nil {
		t.Fatalf("parse observe config: %v", err)
	}
	if cfg.Mode != service.ResponseEngineObserve {
		t.Fatalf("mode=%q, want OBSERVE", cfg.Mode)
	}
	if cfg.Finalizer == nil {
		t.Fatal("observe config did not construct finalizer")
	}
}

func TestParseResponseEngineStartupConfigEnforceBuildsFinalizer(t *testing.T) {
	cfg, err := parseResponseEngineStartupConfig(
		"1",
		"yes",
		"https://response-engine.internal",
		"service-token",
	)
	if err != nil {
		t.Fatalf("parse enforce config: %v", err)
	}
	if cfg.Mode != service.ResponseEngineEnforce {
		t.Fatalf("mode=%q, want ENFORCE", cfg.Mode)
	}
	if cfg.Finalizer == nil {
		t.Fatal("enforce config did not construct finalizer")
	}
}

func TestParseResponseEngineStartupConfigEnabledRequiresURLAndToken(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		token string
	}{
		{name: "missing url", token: "service-token"},
		{name: "missing token", url: "http://response-engine.internal:8080"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseResponseEngineStartupConfig("true", "", tc.url, tc.token); err == nil {
				t.Fatal("expected startup configuration error")
			}
		})
	}
}

func TestParseResponseEngineStartupConfigRejectsInvalidFlagCombination(t *testing.T) {
	if _, err := parseResponseEngineStartupConfig(
		"",
		"true",
		"http://response-engine.internal:8080",
		"service-token",
	); err == nil {
		t.Fatal("expected enforce-without-enabled error")
	}
}
