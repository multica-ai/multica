package webhooks

import (
	"log/slog"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
)

// FeatureFlagEnvVar is the env var that gates the entire cascade
// webhook subsystem. When unset or set to a falsy value (empty,
// "0", "false", "off", "no") the router is not mounted at all and
// /webhooks/{source} returns 404. PR8 will flip this to "true" in
// staging first, then in prod after stability.
const FeatureFlagEnvVar = "MULTICA_CASCADE_WEBHOOK_ENABLED"

// envEnabled returns true when the feature flag env var is set to a
// truthy value. The parser is deliberately strict — only the listed
// truthy values count, so a typo like "ture" is treated as off.
// Centralised here so tests, the main router wiring, and any future
// admin endpoint that surfaces the flag all read it the same way.
func envEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(FeatureFlagEnvVar))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// MountFromEnv reads the feature flag and, when enabled, constructs a
// Router with all stub source adapters registered and mounts it on
// the supplied Chi router. Returns the *Router so the caller can log
// "webhooks subsystem active with N sources" at startup. Returns nil
// when the flag is off — callers should NOT proxy mount the parent
// onto anything; the route literally does not exist on the server.
//
// This is the single integration point cmd/server/router.go uses.
// Future PRs (PR3 replaces GitHubStub with real GitHubSource) only
// touch this function's body — call sites in cmd/server stay
// unchanged.
func MountFromEnv(parent chi.Router, logger *slog.Logger) *Router {
	if !envEnabled() {
		return nil
	}
	r := NewRouter(logger)
	r.Register(GitHubStub())
	r.Register(LinearStub())
	r.Register(SlackStub())
	r.Register(GitLabStub())
	r.Mount(parent)
	return r
}
