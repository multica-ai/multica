package notify

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

func AppURL() string {
	for _, key := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
			return strings.TrimRight(raw, "/")
		}
	}
	return "https://app.multica.ai"
}

func IssuePath(workspaceSlug, issueID string) string {
	return fmt.Sprintf(
		"/%s/issues/%s",
		url.PathEscape(workspaceSlug),
		url.PathEscape(issueID),
	)
}

func CommentPath(workspaceSlug, issueID, commentID string) string {
	path := IssuePath(workspaceSlug, issueID)
	if commentID == "" {
		return path
	}
	return fmt.Sprintf("%s?comment=%s", path, url.QueryEscape(commentID))
}

func IssueURL(baseURL, workspaceSlug, issueID string) string {
	return strings.TrimRight(baseURL, "/") + IssuePath(workspaceSlug, issueID)
}

func CommentURL(baseURL, workspaceSlug, issueID, commentID string) string {
	return strings.TrimRight(baseURL, "/") + CommentPath(workspaceSlug, issueID, commentID)
}
