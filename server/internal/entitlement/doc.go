// Package entitlement fetches and caches workspace-scoped enforcement policy
// from Multica Cloud. It is intentionally a mechanical policy consumer: plan
// names, subscription interpretation, and commercial limits are resolved by
// Cloud and never appear here.
//
// A zero Config is disabled. Disabled, unavailable, invalid, or expired policy
// always returns ActionOff, so merely importing or constructing this package
// cannot change product behavior. A configured MULTICA_CLOUD_URL connects the
// production consumer.
package entitlement
