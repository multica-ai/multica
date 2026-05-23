package githubprheal

import (
	"regexp"
	"testing"
)

func TestIdentifierBoundaryRegex(t *testing.T) {
	re := regexp.MustCompile(`(?i)` + IdentifierBoundaryRegex("JEH-1590"))
	matches := []string{
		"JEH-1590: Merge upstream/main",
		"fix/jeh-1590-upstream",
		"See (JEH-1590)",
	}
	for _, input := range matches {
		if !re.MatchString(input) {
			t.Errorf("expected %q to match", input)
		}
	}

	nonMatches := []string{
		"JEH-15900",
		"XJEH-1590",
		"JEH-1590A",
	}
	for _, input := range nonMatches {
		if re.MatchString(input) {
			t.Errorf("expected %q not to match", input)
		}
	}
}
