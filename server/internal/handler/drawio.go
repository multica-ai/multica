package handler

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultDrawioViewerURL is the public draw.io viewer used to render
// ```drawio fences and .drawio attachments when DRAWIO_VIEWER_URL is unset.
const DefaultDrawioViewerURL = "https://viewer.diagrams.net"

// ResolveDrawioViewerURL returns the viewer base URL advertised to clients in
// /api/config, or "" when the embedded preview is off. An empty rawURL selects
// DefaultDrawioViewerURL. An invalid rawURL turns the preview off and returns
// the reason, so a typo never points browsers at an unintended origin.
func ResolveDrawioViewerURL(rawURL string, disabled bool) (string, error) {
	if disabled {
		return "", nil
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return DefaultDrawioViewerURL, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse DRAWIO_VIEWER_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("DRAWIO_VIEWER_URL must use http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("DRAWIO_VIEWER_URL must be an absolute URL with a host")
	}
	if u.User != nil {
		return "", fmt.Errorf("DRAWIO_VIEWER_URL must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("DRAWIO_VIEWER_URL must not contain a query or fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
