package channel

import (
	"net/url"
	"strings"
)

// IssueWebLink builds the Multica web deep link for a channel issue result.
// Current web routes are workspace-scoped: /{workspaceSlug}/issues/{identifier}.
// When the slug is unavailable, keep the legacy /issues/{identifier} fallback so
// older or partially-populated dispatch results still produce a usable best-effort URL.
func IssueWebLink(appURL, workspaceSlug, identifier string) string {
	if appURL == "" || identifier == "" {
		return ""
	}
	base := strings.TrimRight(appURL, "/")
	issuePath := "/issues/" + url.PathEscape(identifier)
	workspaceSlug = strings.Trim(strings.TrimSpace(workspaceSlug), "/")
	if workspaceSlug == "" {
		return base + issuePath
	}
	return base + "/" + url.PathEscape(workspaceSlug) + issuePath
}
