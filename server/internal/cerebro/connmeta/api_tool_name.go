package connmeta

import (
	"regexp"
	"strings"
)

var nonToolChar = regexp.MustCompile(`[^a-z0-9]+`)

// APIEndpointToolName returns the canonical callable name for an API endpoint.
func APIEndpointToolName(connection, method, path string) string {
	segment := func(value string) string {
		return strings.Trim(nonToolChar.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "_"), "_")
	}
	if connection, endpoint := segment(connection), segment(method+" "+path); connection != "" && endpoint != "" {
		return connection + "__" + endpoint
	}
	return ""
}
