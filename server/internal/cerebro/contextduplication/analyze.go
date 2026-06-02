// Package contextduplication scores duplicate content across named prompt sections.
package contextduplication

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Section is one logical block of the composed agent brief (e.g. "## Workspace Context").
type Section struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Tokens  int64  `json:"tokens"`
}

// OverlapPair names two sections that share repeated sentences.
type OverlapPair struct {
	SectionA    string  `json:"section_a"`
	SectionB    string  `json:"section_b"`
	OverlapPct  float64 `json:"overlap_pct"`
	SampleText  string  `json:"sample_text,omitempty"`
}

// Result is the duplication score for one composed prompt.
type Result struct {
	TotalTokens         int64         `json:"total_tokens"`
	DuplicateTokens     int64         `json:"duplicate_tokens"`
	DuplicatePct        float64       `json:"duplicate_pct"`
	TopOverlapSections  []OverlapPair `json:"top_overlap_sections"`
}

var sectionHeading = regexp.MustCompile(`(?m)^## +(.+)$`)

// ParseSections splits markdown on level-2 headings. Content before the first heading
// is labeled "Preamble".
func ParseSections(markdown string) []Section {
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return nil
	}
	indices := sectionHeading.FindAllStringSubmatchIndex(markdown, -1)
	if len(indices) == 0 {
		return []Section{{
			ID:      slugSection("Preamble"),
			Title:   "Preamble",
			Content: markdown,
			Tokens:  EstimateTokens(markdown),
		}}
	}

	var sections []Section
	if indices[0][0] > 0 {
		preamble := strings.TrimSpace(markdown[:indices[0][0]])
		if preamble != "" {
			sections = append(sections, Section{
				ID:      "preamble",
				Title:   "Preamble",
				Content: preamble,
				Tokens:  EstimateTokens(preamble),
			})
		}
	}
	for i, loc := range indices {
		title := markdown[loc[2]:loc[3]]
		bodyStart := loc[1]
		bodyEnd := len(markdown)
		if i+1 < len(indices) {
			bodyEnd = indices[i+1][0]
		}
		body := strings.TrimSpace(markdown[bodyStart:bodyEnd])
		sections = append(sections, Section{
			ID:      slugSection(title),
			Title:   title,
			Content: body,
			Tokens:  EstimateTokens(body),
		})
	}
	return sections
}

// Analyze scores duplicate sentences across sections. A sentence that appears in
// more than one section contributes its token estimate to DuplicateTokens.
func Analyze(sections []Section) Result {
	var total, duplicate int64
	for i := range sections {
		if sections[i].Tokens == 0 {
			sections[i].Tokens = EstimateTokens(sections[i].Content)
		}
		total += sections[i].Tokens
	}
	if len(sections) < 2 {
		return Result{TotalTokens: total}
	}

	type sentKey struct {
		norm string
	}
	// sentence norm -> section IDs where it appears
	occurrences := make(map[string][]string)
	for _, sec := range sections {
		seenInSec := make(map[string]struct{})
		for _, sent := range splitSentences(sec.Content) {
			norm := normalizeSentence(sent)
			if norm == "" || len(norm) < 8 {
				continue
			}
			if _, ok := seenInSec[norm]; ok {
				continue
			}
			seenInSec[norm] = struct{}{}
			occurrences[norm] = append(occurrences[norm], sec.ID)
		}
	}

	// Pairwise overlap counts for top overlaps
	pairShared := make(map[[2]string]int64)
	pairSample := make(map[[2]string]string)

	for norm, secs := range occurrences {
		tok := EstimateTokens(norm)
		if len(secs) > 1 {
			duplicate += int64(len(secs)-1) * tok
		}
		if len(secs) < 2 {
			continue
		}
		sort.Strings(secs)
		for i := 0; i < len(secs); i++ {
			for j := i + 1; j < len(secs); j++ {
				pair := [2]string{secs[i], secs[j]}
				pairShared[pair] += tok
				if pairSample[pair] == "" {
					pairSample[pair] = truncateSample(norm, 120)
				}
			}
		}
	}

	var pairs []OverlapPair
	for pair, shared := range pairShared {
		if shared <= 0 {
			continue
		}
		aTok, bTok := sectionTokens(sections, pair[0]), sectionTokens(sections, pair[1])
		denom := aTok + bTok
		pct := 0.0
		if denom > 0 {
			pct = float64(shared*2) / float64(denom) * 100
		}
		pairs = append(pairs, OverlapPair{
			SectionA:   pair[0],
			SectionB:   pair[1],
			OverlapPct: pct,
			SampleText: pairSample[pair],
		})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].OverlapPct > pairs[j].OverlapPct
	})
	if len(pairs) > 3 {
		pairs = pairs[:3]
	}

	dupPct := 0.0
	if total > 0 {
		dupPct = float64(duplicate) / float64(total) * 100
	}
	return Result{
		TotalTokens:        total,
		DuplicateTokens:    duplicate,
		DuplicatePct:       dupPct,
		TopOverlapSections: pairs,
	}
}

// EstimateTokens approximates tokens as chars/4 (same rule as cost savings runtime).
func EstimateTokens(text string) int64 {
	n := len([]rune(strings.TrimSpace(text)))
	if n == 0 {
		return 0
	}
	return int64((n + 3) / 4)
}

func splitSentences(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, part := range strings.Split(line, ".") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func normalizeSentence(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func slugSection(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "section"
	}
	return s
}

func sectionTokens(sections []Section, id string) int64 {
	for _, s := range sections {
		if s.ID == id {
			return s.Tokens
		}
	}
	return 0
}

func truncateSample(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
