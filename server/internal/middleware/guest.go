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
	// Which paid features the plan includes: not sensitive, and shared
	// components read it to render locked states.
	"GET /api/workspaces/*/features",
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
	"GET /api/issues/*/duplicates",

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
	// The same delete under the path clients use when they promise to keep
	// the replies (#8296); the handler still allows only the author.
	"DELETE /api/comments/*/keep-replies",
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
	"GET /api/inbox/*/*",
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

// Reads the workspace shell makes on every page (presence, chat badges,
// runtime health, mention and assignee lists) answer a guest with an empty
// result instead of 403: the guest has none of these, and an empty answer
// keeps every component that asks from failing.
var guestEmptyReads = []struct {
	route guestRoute
	body  string
}{
	{compileGuestRoutes([]string{"GET /api/agent-task-snapshot"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/assignee-frequency"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/chat/sessions"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/chat/pending-tasks"})[0], `{"tasks":[]}`},
	{compileGuestRoutes([]string{"GET /api/chat/pending-tasks/has-any"})[0], `{"has_pending":false}`},
	{compileGuestRoutes([]string{"GET /api/runtimes"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/squads"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/squads/involved"})[0], `{"squad_ids":[]}`},
	{compileGuestRoutes([]string{"GET /api/working-agents"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/issue-wakeup-summaries"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/issue-wakeup-paused"})[0], `[]`},
	// Saved views and project folders organize the whole workspace; a guest
	// has neither.
	{compileGuestRoutes([]string{"GET /api/issue-views"})[0], `[]`},
	{compileGuestRoutes([]string{"GET /api/project-folders"})[0], `{"folders":[],"filed":[]}`},
	// The members tab can mount for a moment before settings knows the viewer
	// is a guest.
	{compileGuestRoutes([]string{"GET /api/workspaces/*/invitations"})[0], `[]`},
}

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

func (route guestRoute) matches(method string, segments []string) bool {
	if route.method != method || len(route.segments) != len(segments) {
		return false
	}
	for i, s := range route.segments {
		if s != "*" && s != segments[i] {
			return false
		}
	}
	return true
}

// GuestMayRequest reports whether a guest may make this request at all.
func GuestMayRequest(r *http.Request) bool {
	segments := splitPath(r.URL.Path)
	for _, route := range guestRoutes {
		if route.matches(r.Method, segments) {
			return true
		}
	}
	return false
}

// RefuseGuest answers a guest request GuestMayRequest rejected: an empty
// result for the shell's background reads, 403 for everything else.
func RefuseGuest(w http.ResponseWriter, r *http.Request) {
	segments := splitPath(r.URL.Path)
	for _, read := range guestEmptyReads {
		if read.route.matches(r.Method, segments) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(read.body))
			return
		}
	}
	writeError(w, http.StatusForbidden, "insufficient permissions")
}
