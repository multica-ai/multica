// Package featureflag is a framework-level feature flag library for the
// multica backend.
//
// It implements the canonical Toggle Point / Toggle Router / Toggle
// Configuration separation described by Martin Fowler:
//
//	business code -> Service.IsEnabled(ctx, key, default)    // Toggle Point
//	                 Service                                  // Toggle Router
//	                 Provider (Static/Env/Chain/custom)       // Toggle Configuration
//
// Design goals:
//
//   - Business code never speaks to a provider directly; it always asks the
//     Service. This keeps the decision point decoupled from the decision
//     logic so the same Toggle Point can be backed by a YAML file today, a
//     database tomorrow, and an A/B router after that, with no caller
//     changes.
//   - Always-on safety: a missing provider, a missing key, or a misconfigured
//     rule must never crash callers. Every public entry point returns the
//     supplied default in that case and records a Reason so the failure is
//     observable.
//   - Deterministic percent rollouts: the same (key, identifier) pair always
//     evaluates to the same bucket so a user does not flip in and out of an
//     experiment across requests.
//
// Wiring:
//
// The standard way to construct the Service inside the multica server is
// featureflag.NewServiceFromEnv, which reads MULTICA_FEATURE_FLAGS_FILE for
// the YAML rule set and layers an EnvProvider on top so individual flags
// can be overridden at runtime via FF_<KEY> env vars. The core types only
// depend on the standard library; the YAML loader pulls in gopkg.in/yaml.v3
// which is already a server-level dependency.
//
// See server/pkg/featureflag/service.go for the public Service API and
// docs/feature-flags.md for end-to-end usage examples.
package featureflag
