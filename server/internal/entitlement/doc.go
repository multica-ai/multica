// Package entitlement fetches and caches workspace-scoped enforcement policy
// from Multica Cloud. It is intentionally a mechanical policy consumer: plan
// names, subscription interpretation, rollout cohorts, dates, and commercial
// limits are resolved by Cloud and never appear here.
//
// A zero Config is disabled. Disabled, unavailable, invalid, or expired policy
// always returns ActionOff, so merely importing or constructing this package
// cannot change product behavior. No production caller is wired in PR-2.
package entitlement
