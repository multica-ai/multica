package util

import (
	"net/url"
	"strings"
)

const (
	attachmentDownloadPathPrefix = "/api/attachments/"
	attachmentDownloadPathSuffix = "/download"
)

// AttachmentDownloadPath returns the durable API path that may be persisted in
// Markdown bodies. Keep construction and parsing of this route in one place so
// capture and rendering code do not duplicate its syntax.
func AttachmentDownloadPath(attachmentID string) string {
	return attachmentDownloadPathPrefix + attachmentID + attachmentDownloadPathSuffix
}

// AttachmentIDFromDownloadURL extracts an attachment UUID from the durable
// download route. Site-relative and absolute HTTP(S) URLs are accepted, as are
// query strings and fragments; other paths and schemes are rejected.
func AttachmentIDFromDownloadURL(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "" && !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", false
	}
	if parsed.Host != "" && parsed.Scheme == "" {
		return "", false
	}
	path := parsed.Path
	if !strings.HasPrefix(path, attachmentDownloadPathPrefix) || !strings.HasSuffix(path, attachmentDownloadPathSuffix) {
		return "", false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, attachmentDownloadPathPrefix), attachmentDownloadPathSuffix)
	if strings.Contains(id, "/") {
		return "", false
	}
	parsedID, err := ParseUUID(id)
	if err != nil {
		return "", false
	}
	return UUIDToString(parsedID), true
}
