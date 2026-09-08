package channel

import (
	"net/url"
	"strings"
)

// IssueWebLink builds the Multica web deep link for a channel issue result.
// Web routes are workspace-scoped — /{workspaceSlug}/issues/{identifier} — so a
// link missing the slug cannot resolve to the issue and is not worth sending;
// any empty part yields an empty string and the caller replies without a link.
// That is deliberate for the adapters that will reuse this helper: an adapter
// that forgets to plumb the slug through loses its link loudly, instead of
// silently shipping the unroutable legacy /issues/{identifier} form again.
func IssueWebLink(appURL, workspaceSlug, identifier string) string {
	if appURL == "" || workspaceSlug == "" || identifier == "" {
		return ""
	}
	return strings.TrimRight(appURL, "/") + "/" + url.PathEscape(workspaceSlug) +
		"/issues/" + url.PathEscape(identifier)
}
