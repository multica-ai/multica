package execenv

import "strings"

// headingIndex returns the start of a markdown heading as its own line so
// inline references like "see ## Available Commands" are not mistaken for
// the section itself.
func headingIndex(s, heading string) int {
	if strings.HasPrefix(s, heading+"\n") {
		return 0
	}
	needle := "\n" + heading + "\n"
	i := strings.Index(s, needle)
	if i < 0 {
		return -1
	}
	return i + 1
}

// headingSection returns the heading and its body up to the next heading.
func headingSection(s, heading string) string {
	i := headingIndex(s, heading)
	if i < 0 {
		return ""
	}
	rest := s[i:]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return rest
	}
	body := rest[nl+1:]
	next := -1
	for _, prefix := range []string{"\n## ", "\n### "} {
		j := strings.Index(body, prefix)
		if j >= 0 && (next < 0 || j < next) {
			next = j
		}
	}
	if next < 0 {
		return rest
	}
	return rest[:nl+1+next]
}
