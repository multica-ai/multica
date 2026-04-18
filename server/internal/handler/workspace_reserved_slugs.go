package handler

// reservedSlugs are workspace slugs that would collide with frontend top-level
// routes, platform features, or web standards. The frontend URL shape is
// /{workspaceSlug}/... so any slug that matches a top-level route or a
// system-significant name is rejected at workspace creation time.
//
// Keep this list in sync with packages/core/paths/reserved-slugs.ts.
//
// Convention for new global routes: use a single word (`/login`, `/inbox`)
// or `/{noun}/{verb}` (`/workspaces/new`). Hyphenated root-level word groups
// (`/new-workspace`, `/create-team`) collide with common user workspace names.
var reservedSlugs = map[string]bool{
	// Auth flow
	"login":      true,
	"logout":     true,
	"signin":     true,
	"signout":    true,
	"signup":     true,
	"auth":       true,
	"oauth":      true,
	"callback":   true,
	"invite":     true,
	"verify":     true,
	"reset":      true,
	"password":   true,
	"onboarding": true, // historical, kept reserved post-removal

	// Platform / marketing routes (current + likely-future)
	"api":       true,
	"admin":     true,
	"help":      true,
	"about":     true,
	"pricing":   true,
	"changelog": true,
	"docs":      true,
	"support":   true,
	"status":    true,
	"legal":     true,
	"privacy":   true,
	"terms":     true,
	"security":  true,
	"contact":   true,
	"blog":      true,
	"careers":   true,
	"press":     true,
	"download":  true,

	// Dashboard / workspace route segments
	"issues":     true,
	"projects":   true,
	"autopilots": true,
	"agents":     true,
	"inbox":      true,
	"my-issues":  true,
	"runtimes":   true,
	"skills":     true,
	"settings":   true,
	"workspaces": true, // global /workspaces/new workspace creation page
	"teams":      true, // reserved for future team management routes

	// RFC 2142 — privileged email mailboxes
	"postmaster": true,
	"abuse":      true,
	"noreply":    true,
	"webmaster":  true,
	"hostmaster": true,

	// Hostname / subdomain confusables
	"mail":    true,
	"ftp":     true,
	"static":  true,
	"cdn":     true,
	"assets":  true,
	"public":  true,
	"files":   true,
	"uploads": true,

	// Next.js / web standards
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
