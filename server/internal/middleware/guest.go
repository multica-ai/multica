package middleware

import (
	"net/http"
	"strings"
)

// Guests (MUL-9420) are deny-by-default: a workspace route answers a guest
// only when it is listed here. Everything a listed route returns is still
// narrowed to the guest's projects by projectaccess, so this list decides
// which kinds of thing a guest may touch, not which rows.
//
// A pattern is "METHOD /path", where "*" matches exactly one path segment.
var guestRoutes = compileGuestRoutes([]string{
	// The workspace shell: its record, its people (names for mentions and
	// assignees), and leaving it.
	"GET /api/workspaces/*",
	"GET /api/workspaces/*/members",
	"POST /api/workspaces/*/leave",

	// Reading issues in the guest's projects.
	"GET /api/issues",
	"POST /api/issues/query",
	"POST /api/issues/table/groups",
	"POST /api/issues/table/rows",
	"POST /api/issues/table/facets",
	"GET /api/issues/search",
	"GET /api/issues/child-progress",
	"GET /api/issues/children",
	"GET /api/issues/grouped",
	"GET /api/issues/unread",
	"GET /api/issues/subscribed",
	"GET /api/issues/pull-requests",
	"GET /api/issues/*",
	"GET /api/issues/*/comments",
	"GET /api/issues/*/timeline",
	"GET /api/issues/*/subscribers",
	"GET /api/issues/*/attachments",
	"GET /api/issues/*/children",
	"GET /api/issues/*/labels",
	"GET /api/issues/*/metadata",
	"GET /api/issues/*/pull-requests",
	"GET /api/issues/*/active-task",

	// Commenting, reacting and following.
	"POST /api/issues/*/comments",
	"POST /api/issues/*/reactions",
	"DELETE /api/issues/*/reactions",
	"POST /api/issues/*/subscribe",
	"POST /api/issues/*/unsubscribe",
	"POST /api/issues/*/read",
	"POST /api/issues/*/unread",
	"PUT /api/comments/*",
	"DELETE /api/comments/*",
	"POST /api/comments/*/reactions",
	"DELETE /api/comments/*/reactions",

	// Projects shared with the guest.
	"GET /api/projects",
	"GET /api/projects/search",
	"GET /api/projects/*",
	"GET /api/projects/*/resources",
	"GET /api/projects/*/subscription",
	"PUT /api/projects/*/subscription",
	"DELETE /api/projects/*/subscription",

	// What an issue page needs to render.
	"GET /api/labels",
	"GET /api/issue-statuses",
	"GET /api/issue-workflows",
	"GET /api/properties",
	"GET /api/agents",
	"GET /api/attachments/*",
	"GET /api/attachments/*/content",

	// The guest's own inbox, pins, view preferences and notification settings.
	"GET /api/inbox",
	"GET /api/inbox/*",
	"POST /api/inbox/*",
	"POST /api/inbox/*/*",
	"POST /api/inbox/issues/*/read",
	"DELETE /api/inbox/*",
	"GET /api/notification-preferences",
	"PATCH /api/notification-preferences",
	"PUT /api/notification-preferences",
	"GET /api/pins",
	"POST /api/pins",
	"PUT /api/pins/reorder",
	"DELETE /api/pins/*/*",
	"GET /api/issue-view-preferences",
	"PUT /api/issue-view-preferences",

	// Local search over what the guest can see.
	"GET /api/search-index/manifest",
	"GET /api/search-index/snapshot",
	"POST /api/search-index/changes",

	// User-scoped routes that check membership in the handler.
	"POST /api/upload-file",
	"GET /api/attachments/*/download",
	"GET /api/attachments/*/download-metadata",
})

type guestRoute struct {
	method   string
	segments []string
}

func compileGuestRoutes(patterns []string) []guestRoute {
	routes := make([]guestRoute, 0, len(patterns))
	for _, p := range patterns {
		method, path, ok := strings.Cut(p, " ")
		if !ok {
			panic("guest route without a method: " + p)
		}
		routes = append(routes, guestRoute{method: method, segments: splitPath(path)})
	}
	return routes
}

func splitPath(path string) []string {
	return strings.Split(strings.Trim(path, "/"), "/")
}

// GuestMayRequest reports whether a guest may make this request at all.
func GuestMayRequest(r *http.Request) bool {
	segments := splitPath(r.URL.Path)
	for _, route := range guestRoutes {
		if route.method != r.Method || len(route.segments) != len(segments) {
			continue
		}
		match := true
		for i, s := range route.segments {
			if s != "*" && s != segments[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
