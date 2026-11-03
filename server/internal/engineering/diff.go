// Package engineering computes deterministic baseline diffs without model calls.
package engineering

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var Chapters = []string{"overview", "domain_rules", "architecture", "exclusions_and_risks"}

var headingPattern = regexp.MustCompile(`^ {0,3}(#{1,3})[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)
var explicitKeyPattern = regexp.MustCompile(`^\[([\pL\pN_]+-[^\]\r\n]{1,64})\][ \t]*(.*)$`)

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

type Section struct {
	Anchor string
	Title  string
	Body   string
}

type Change struct {
	Anchor string `json:"anchor"`
	Title  string `json:"title"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type Warning struct {
	Code   string `json:"code"`
	Anchor string `json:"anchor"`
}

type Diff struct {
	Chapter            string    `json:"chapter"`
	BaseRevisionNumber int32     `json:"base_revision_number"`
	Added              []Change  `json:"added"`
	Modified           []Change  `json:"modified"`
	Removed            []Change  `json:"removed"`
	UnchangedCount     int       `json:"unchanged_count"`
	Warnings           []Warning `json:"warnings"`
}

func ValidChapter(chapter string) bool { return slices.Contains(Chapters, chapter) }

func normalize(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// Parse ignores fenced examples and ends each anchor at the next H1-H3 heading.
func Parse(chapter, content string) ([]Section, error) {
	if !ValidChapter(chapter) {
		return nil, &Error{"invalid_request"}
	}
	if len(content) > 65536 {
		return nil, &Error{"baseline_too_large"}
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return nil, &Error{"baseline_content_invalid"}
	}
	sections := []Section{{Anchor: "_preamble"}}
	bodies := []*strings.Builder{new(strings.Builder)}
	seen := map[string]bool{}
	current := 0
	parent := ""
	fence := ""
	for _, line := range strings.Split(normalize(content), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		inFence := fence != ""
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:1]
			n := len(trimmed) - len(strings.TrimLeft(trimmed, marker))
			if fence == "" {
				fence = strings.Repeat(marker, n)
			} else if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(strings.TrimLeft(trimmed, marker)) == "" {
				fence = ""
			}
			inFence = true
		}
		if !inFence {
			if match := headingPattern.FindStringSubmatch(line); match != nil {
				if current == 0 && normalize(bodies[0].String()) != "" {
					seen["_preamble"] = true
				}
				title := strings.Join(strings.Fields(match[2]), " ")
				key := title
				if explicit := explicitKeyPattern.FindStringSubmatch(strings.TrimSpace(match[2])); explicit != nil {
					key, title = explicit[1], explicit[2]
				} else if len(match[1]) == 3 && parent != "" {
					key = parent + " / " + key
				}
				base := key
				for n := 2; seen[key]; n++ {
					key = base + " (" + strconv.Itoa(n) + ")"
				}
				seen[key] = true
				if len(match[1]) < 3 {
					parent = key
				}
				sections = append(sections, Section{Anchor: key, Title: title})
				bodies = append(bodies, new(strings.Builder))
				current = len(sections) - 1
				continue
			}
		}
		if current >= 0 {
			bodies[current].WriteString(line)
			bodies[current].WriteByte('\n')
		}
	}
	for i := range sections {
		sections[i].Body = normalize(bodies[i].String())
	}
	if sections[0].Body == "" {
		sections = sections[1:]
	}
	return sections, nil
}

func Compute(chapter, before, after string, baseNumber int32) (Diff, error) {
	d := Diff{Chapter: chapter, BaseRevisionNumber: baseNumber, Added: []Change{}, Modified: []Change{}, Removed: []Change{}, Warnings: []Warning{}}
	old, err := Parse(chapter, before)
	if err != nil {
		return d, err
	}
	next, err := Parse(chapter, after)
	if err != nil {
		return d, err
	}
	oldMap, nextMap := map[string]Section{}, map[string]Section{}
	for _, s := range old {
		oldMap[s.Anchor] = s
	}
	for _, s := range next {
		nextMap[s.Anchor] = s
	}
	for _, s := range next {
		previous, exists := oldMap[s.Anchor]
		if !exists {
			d.Added = append(d.Added, Change{Anchor: s.Anchor, Title: s.Title, After: s.Body})
			continue
		}
		if previous.Title == s.Title && previous.Body == s.Body {
			d.UnchangedCount++
			continue
		}
		// Include titles in the compared text so title-only edits remain visible.
		oldText, newText := previous.Body, s.Body
		if previous.Title != s.Title {
			oldText = previous.Title + "\n" + oldText
			newText = s.Title + "\n" + newText
		}
		d.Modified = append(d.Modified, Change{Anchor: s.Anchor, Title: s.Title, Before: oldText, After: newText})
	}
	for _, s := range old {
		if _, exists := nextMap[s.Anchor]; exists {
			continue
		}
		d.Removed = append(d.Removed, Change{Anchor: s.Anchor, Title: s.Title, Before: s.Body})
	}
	return d, nil
}
