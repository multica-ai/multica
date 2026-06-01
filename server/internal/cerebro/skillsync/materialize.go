package skillsync

import (
	"fmt"
	"regexp"
	"strings"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// skillSlug converts a skill name into the directory name used in the archive
// (lowercase, non-alphanumeric runs collapsed to a single dash). Mirrors the
// daemon's sanitizeSkillName so a skill materializes to the same slug the
// runtime would use.
func skillSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "skill"
	}
	return s
}

// frontmatterDomain extracts the `domain:` scalar from a SKILL.md frontmatter
// block, lowercased, or "" when absent. firtal-skills places a skill by the
// folder it lives in; this lets a skill self-declare its home domain.
func frontmatterDomain(content string) string {
	body, ok := frontmatterBody(content)
	if !ok {
		return ""
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "domain:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "domain:"))
		v = strings.Trim(v, `"'`)
		return strings.ToLower(v)
	}
	return ""
}

// frontmatterBody returns the YAML body between the opening and closing `---`
// delimiters, and whether a well-formed block was found.
func frontmatterBody(content string) (string, bool) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", false
	}
	start := strings.IndexByte(content, '\n') + 1
	closeIdx := strings.Index(content[start:], "\n---")
	if closeIdx < 0 {
		return "", false
	}
	return content[start : start+closeIdx], true
}

// hasFrontmatterName reports whether the frontmatter body carries a parseable,
// non-empty `name:` scalar.
func hasFrontmatterName(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "name:") {
			continue
		}
		v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), `"'`)
		if v != "" {
			return true
		}
	}
	return false
}

// ensureFrontmatter guarantees SKILL.md leads with a frontmatter block that
// carries a non-empty `name`. It mirrors the daemon's ensureSkillFrontmatter:
// runtimes (and the archive's catalog generator) silently drop a SKILL.md
// whose frontmatter has no name. Existing frontmatter is preserved verbatim so
// the rich curated metadata (owner, tags, quality, domain) survives the
// round-trip.
func ensureFrontmatter(content, slug, description string) string {
	body, ok := frontmatterBody(content)
	if !ok {
		var b strings.Builder
		b.WriteString("---\n")
		fmt.Fprintf(&b, "name: %s\n", slug)
		if d := strings.TrimSpace(description); d != "" {
			fmt.Fprintf(&b, "description: %s\n", yamlEscapeInline(d))
		}
		b.WriteString("---\n\n")
		b.WriteString(content)
		return b.String()
	}
	if hasFrontmatterName(body) {
		return content
	}
	// Frontmatter exists but lacks a name — inject one as the first key and
	// keep the rest verbatim.
	bodyStart := strings.IndexByte(content, '\n') + 1
	return content[:bodyStart] + "name: " + slug + "\n" + content[bodyStart:]
}

// yamlEscapeInline returns a double-quoted YAML scalar that always parses as a
// string regardless of the input's characters.
func yamlEscapeInline(s string) string {
	flat := strings.ReplaceAll(s, "\r\n", " ")
	flat = strings.ReplaceAll(flat, "\n", " ")
	flat = strings.ReplaceAll(flat, "\r", " ")
	escaped := strings.ReplaceAll(flat, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

// archiveDomain resolves the domain folder a skill belongs to: an existing
// placement (existingDomain, found by slug) always wins so curation is never
// disturbed; otherwise the frontmatter domain if it names a known domain;
// otherwise the configured default.
func archiveDomain(content, existingDomain, defaultDomain string) string {
	if existingDomain != "" {
		return existingDomain
	}
	if d := frontmatterDomain(content); archiveDomains[d] {
		return d
	}
	return defaultDomain
}
