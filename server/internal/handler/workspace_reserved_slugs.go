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
	"multica":   true, // brand name — prevent impersonation workspaces
	"www":       true, // hostname confusable; never a legitimate workspace slug
	"new":       true, // ambiguous verb-as-slug; reserved for future global create routes
	"home":      true, // likely-future marketing/landing entry
	"homepage":  true, // existing /homepage landing variant in apps/web
	"dashboard": true, // standard SaaS entry; likely-future global route
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

	// Account / billing (likely-future global routes in the avatar menu)
	"profile":       true,
	"account":       true,
	"billing":       true,
	"notifications": true,
	"search":        true,
	"members":       true,

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

	// API / integration prefixes. `api` above already covers /api/*; these
	// guard against future top-level API alias routes (e.g. /v1, /graphql)
	// and against accidental workspace slugs that read like API identifiers.
	"v1":       true,
	"v2":       true,
	"graphql":  true,
	"webhooks": true,
	"sdk":      true,
	"tokens":   true,
	"cli":      true,

	// Backend ops / observability. `/health`, `/readyz`, `/healthz`, `/ws`, and `/sse`
	// exist on the backend
	// host; reserving them on the workspace slug space prevents naming
	// confusion if/when these paths are ever proxied through the web origin.
	"health":  true,
	"readyz":  true,
	"healthz": true,
	"ws":      true,
	"sse":     true,
	"metrics": true,
	"ping":    true,

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

	// Next.js / web standards. These entries contain characters (dots,
	// underscores) that today's slug regex `^[a-z0-9]+(?:-[a-z0-9]+)*$`
	// already rejects at the format-validation step — so `isReservedSlug`
	// never actually matches them. They are kept as defense-in-depth so
	// that if the slug regex is ever relaxed (e.g. to support dotted
	// corporate slugs like `acme.io`), these system paths stay protected.
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
