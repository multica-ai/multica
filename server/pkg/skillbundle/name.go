package skillbundle

import (
	"regexp"
	"strings"
)

var nonAlphaNumericName = regexp.MustCompile(`[^a-z0-9]+`)

// NormalizeName converts a display name into the directory slug used by
// runtime skill materializers.
func NormalizeName(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = nonAlphaNumericName.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "skill"
	}
	return slug
}
