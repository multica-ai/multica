package handler

// reservedSlugs are workspace slugs that would collide with frontend top-level
// routes. The frontend URL shape is /{workspaceSlug}/... so any slug that
// matches a top-level route (e.g. /login, /api) would be unreachable.
//
// Keep this list in sync with packages/core/paths/reserved-slugs.ts.
var reservedSlugs = map[string]bool{
	// Auth + onboarding routes
	"login":      true,
	"logout":     true,
	"signup":     true,
	"onboarding": true,
	"invite":     true,
	"auth":       true,

	// Reserved for future platform routes
	"api":       true,
	"admin":     true,
	"help":      true,
	"about":     true,
	"pricing":   true,
	"changelog": true,

	// Dashboard route segments. Reserved to avoid URL ambiguity — a
	// workspace named "issues" with /issues/abc URL reads as either
	// "issue abc in workspace 'issues'" or "issue abc in some workspace".
	"issues":    true,
	"projects":  true,
	"autopilots": true,
	"agents":    true,
	"inbox":     true,
	"my-issues": true,
	"runtimes":  true,
	"skills":    true,
	"settings":  true,

	// Next.js / hosting internals
	"_next":         true,
	"favicon.ico":   true,
	"robots.txt":    true,
	"sitemap.xml":   true,
	"manifest.json": true,
	".well-known":   true,
}

func isReservedSlug(slug string) bool {
	return reservedSlugs[slug]
}
