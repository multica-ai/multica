// Package connectedapps owns how agents use external apps (MUL-8516): which
// account a run acts as and what each tool call is allowed to do.
//
// Agents never see a provider credential. A run reaches its apps through the
// Multica MCP gateway, which resolves the account, classifies the tool, applies
// the workspace and personal rules, and only then executes through the
// provider.
package connectedapps

import "strings"

// Account scopes an agent app can act through.
const (
	ScopeTeam     = "team"
	ScopePersonal = "personal"
)

// Agent permission levels for one app.
const (
	LevelRead          = "read"
	LevelConfirmWrites = "confirm_writes"
	LevelWrite         = "write"
)

// Workspace catalog modes.
const (
	CatalogAll     = "all"
	CatalogCurated = "curated"
	CatalogCustom  = "custom"
)

// CuratedApp is an app Multica has tested end to end. Curated apps carry a
// badge in the catalog, a recommended account type, and hand-checked tool
// classes; every other app works through automatic classification.
type CuratedApp struct {
	Slug string
	// Scope is the recommended account type: team, personal, or "" for either.
	Scope string
	// Overrides pins the class of specific tools, keyed by upper-case tool slug.
	Overrides map[string]string
}

var curatedApps = []CuratedApp{
	{Slug: "gmail", Scope: ScopePersonal},
	{Slug: "googlecalendar", Scope: ScopePersonal, Overrides: map[string]string{
		"GOOGLECALENDAR_CREATE_EVENT":    ClassSend,
		"GOOGLECALENDAR_UPDATE_EVENT":    ClassSend,
		"GOOGLECALENDAR_PATCH_EVENT":     ClassSend,
		"GOOGLECALENDAR_QUICK_ADD":       ClassSend,
		"GOOGLECALENDAR_FIND_FREE_SLOTS": ClassRead,
	}},
	{Slug: "outlook", Scope: ScopePersonal},
	{Slug: "slack", Scope: ScopePersonal},
	{Slug: "notion"},
	{Slug: "googledrive"},
	{Slug: "googledocs"},
	{Slug: "googlesheets"},
	{Slug: "confluence"},
	{Slug: "linear"},
	{Slug: "jira"},
	{Slug: "asana"},
	{Slug: "figma"},
	{Slug: "sentry", Scope: ScopeTeam},
	{Slug: "posthog", Scope: ScopeTeam},
	{Slug: "hubspot", Scope: ScopeTeam},
	{Slug: "zendesk", Scope: ScopeTeam},
	{Slug: "intercom", Scope: ScopeTeam},
	{Slug: "stripe", Scope: ScopeTeam},
	{Slug: "airtable", Scope: ScopeTeam},
}

var curatedBySlug = func() map[string]CuratedApp {
	m := make(map[string]CuratedApp, len(curatedApps))
	for _, app := range curatedApps {
		m[app.Slug] = app
	}
	return m
}()

// Curated returns the curated entry for slug.
func Curated(slug string) (CuratedApp, bool) {
	app, ok := curatedBySlug[NormalizeSlug(slug)]
	return app, ok
}

// CuratedSlugs returns the curated apps in catalog order.
func CuratedSlugs() []string {
	out := make([]string, 0, len(curatedApps))
	for _, app := range curatedApps {
		out = append(out, app.Slug)
	}
	return out
}

// NormalizeSlug lowercases and trims a toolkit slug.
func NormalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

// ValidScope reports whether scope is an account scope.
func ValidScope(scope string) bool {
	return scope == ScopeTeam || scope == ScopePersonal
}

// DefaultLevel lets newly attached apps run directly for every agent.
func DefaultLevel(_ string) string {
	return LevelWrite
}

// ValidLevel reports whether level is an agent permission level.
func ValidLevel(level string) bool {
	return level == LevelRead || level == LevelConfirmWrites || level == LevelWrite
}
