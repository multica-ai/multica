package agentoffice

// Agent Office managed region in repo CLAUDE.md/AGENTS.md files (FIR-1775,
// docs/agents/agent-office.md §10). The region keeps Agent Office-governed
// guidance physically inside the repo file (the runtime reads it off disk,
// devs see it inline) while making the Agent Office tooling the edit surface:
//
//   <!-- agent-office:start v3 sha256:<64-hex> -->
//   ...governed body lines...
//   <!-- agent-office:end -->
//
// The start marker seals the body: the checksum is the SHA-256 of the body in
// canonical form (every line stripped of a trailing \r and terminated by \n).
// A hand-edit that changes the body without re-sealing breaks the checksum,
// which `scripts/validate-agent-office-region.sh` (CI guard, mirrors this
// format byte-for-byte) rejects. Legitimate edits go through
// `multica agent context region-sync`, which re-renders the seal and bumps the
// version. Content outside the markers is human-owned and never touched here.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ManagedRegion is a parsed agent-office region of a repo instruction file.
type ManagedRegion struct {
	// Version is the integer from the start marker (v3 → 3).
	Version int
	// Checksum is the sha256 hex from the start marker (the seal).
	Checksum string
	// Body is the canonical body: the lines strictly between the two marker
	// lines, each stripped of a trailing \r and terminated by \n. Empty when
	// there are no lines between the markers.
	Body string
	// StartLine and EndLine are the 1-based line numbers of the marker lines.
	StartLine int
	EndLine   int
}

var (
	managedRegionStartRe = regexp.MustCompile(`^<!-- agent-office:start v([0-9]+) sha256:([0-9a-f]{64}) -->$`)
	managedRegionEndRe   = regexp.MustCompile(`^<!-- agent-office:end -->$`)
	// Loose detector for marker-looking lines, so a malformed start marker
	// (bad version, truncated checksum) fails parsing instead of silently
	// counting as body text.
	managedRegionAnyStartRe = regexp.MustCompile(`agent-office:start`)
)

// ManagedRegionChecksum returns the seal for a canonical body.
func ManagedRegionChecksum(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// canonicalBody normalizes raw body lines: strip a trailing \r per line,
// terminate every line with \n. nil/empty input → "".
func canonicalBody(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(strings.TrimSuffix(line, "\r"))
		b.WriteString("\n")
	}
	return b.String()
}

// splitLines splits file content into lines without their terminators. A
// trailing \n does not produce a final empty line.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// ParseManagedRegion finds the agent-office region in a file's content.
// Returns (nil, nil) when the file has no region, and an error when the
// region is structurally malformed: a marker-looking start line that does not
// match the exact format, a start without an end, an end without a start, or
// more than one region.
func ParseManagedRegion(content string) (*ManagedRegion, error) {
	lines := splitLines(content)
	var region *ManagedRegion
	var bodyLines []string
	inRegion := false
	startLine := 0
	version := 0
	checksum := ""
	for i, line := range lines {
		trimmed := strings.TrimSuffix(line, "\r")
		if m := managedRegionStartRe.FindStringSubmatch(trimmed); m != nil {
			if inRegion {
				return nil, fmt.Errorf("line %d: nested agent-office:start inside an open region (started line %d)", i+1, startLine)
			}
			if region != nil {
				return nil, fmt.Errorf("line %d: second agent-office region (first at lines %d-%d) — only one region per file", i+1, region.StartLine, region.EndLine)
			}
			inRegion = true
			startLine = i + 1
			version, _ = strconv.Atoi(m[1]) // regexp guarantees digits
			checksum = m[2]
			bodyLines = nil
			continue
		}
		if managedRegionAnyStartRe.MatchString(trimmed) && strings.HasPrefix(trimmed, "<!--") && !inRegion && region == nil {
			return nil, fmt.Errorf("line %d: malformed agent-office:start marker — expected `<!-- agent-office:start vN sha256:<64-hex> -->`", i+1)
		}
		if managedRegionEndRe.MatchString(trimmed) {
			if !inRegion {
				return nil, fmt.Errorf("line %d: agent-office:end without a matching start", i+1)
			}
			region = &ManagedRegion{
				Version:   version,
				Checksum:  checksum,
				Body:      canonicalBody(bodyLines),
				StartLine: startLine,
				EndLine:   i + 1,
			}
			inRegion = false
			continue
		}
		if inRegion {
			bodyLines = append(bodyLines, line)
		}
	}
	if inRegion {
		return nil, fmt.Errorf("line %d: agent-office:start without a matching end", startLine)
	}
	return region, nil
}

// Verify reports whether the region's seal matches its body.
func (r *ManagedRegion) Verify() bool {
	return r != nil && r.Checksum == ManagedRegionChecksum(r.Body)
}

// RenderManagedRegion renders a sealed region block (markers + body, trailing
// \n) for a version and body. The body is canonicalized first, so callers may
// pass unterminated or CRLF text.
func RenderManagedRegion(version int, body string) string {
	canonical := canonicalBody(splitLines(strings.ReplaceAll(body, "\r\n", "\n")))
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- agent-office:start v%d sha256:%s -->\n", version, ManagedRegionChecksum(canonical))
	b.WriteString(canonical)
	b.WriteString("<!-- agent-office:end -->\n")
	return b.String()
}

// UpsertManagedRegion replaces the file's existing region with a freshly
// sealed one, or appends a new region at the end of the file (separated by a
// blank line) when none exists. Content outside the markers is preserved
// byte-for-byte. Returns an error when the existing content is malformed.
func UpsertManagedRegion(content string, version int, body string) (string, error) {
	region, err := ParseManagedRegion(content)
	if err != nil {
		return "", err
	}
	block := RenderManagedRegion(version, body)
	if region == nil {
		if content == "" {
			return block, nil
		}
		sep := "\n"
		if !strings.HasSuffix(content, "\n") {
			sep = "\n\n"
		}
		return content + sep + block, nil
	}
	lines := strings.SplitAfter(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		lineNo := i + 1
		if lineNo < region.StartLine || lineNo > region.EndLine {
			b.WriteString(line)
			continue
		}
		if lineNo == region.StartLine {
			b.WriteString(block)
		}
	}
	return b.String(), nil
}
