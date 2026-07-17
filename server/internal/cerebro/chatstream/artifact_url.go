package chatstream

import (
	"net/url"
	"strings"
)

// ArtifactDocumentURL builds the public web route for one workspace artifact.
func ArtifactDocumentURL(baseURL, workspaceSlug, artifactID string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" || workspaceSlug == "" || artifactID == "" {
		return ""
	}
	return baseURL + "/" + url.PathEscape(workspaceSlug) + "/documents/" + url.PathEscape(artifactID)
}
