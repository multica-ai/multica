package relatedcheck

import (
	"sort"
	"strings"
	"unicode"
)

// maxTerms caps how many keywords reach the candidate query. Every extra term
// is another ILIKE per row, and past a handful they stop discriminating.
const maxTerms = 8

// minTermLen is the shortest word we search on. Shorter words ("api", "ui")
// match too much to be useful as the only signal.
const minTermLen = 4

// stopwords are Danish and English filler that appears in most issue titles and
// would match everything. Kept deliberately small — over-filtering costs recall.
var stopwords = map[string]bool{
	// Danish
	"alle": true, "andre": true, "bliver": true, "denne": true, "dette": true,
	"disse": true, "efter": true, "eller": true, "have": true, "hvad": true,
	"hvor": true, "hvordan": true, "hvornår": true, "ikke": true, "kunne": true,
	"lave": true, "meget": true, "noget": true, "nogle": true, "samme": true,
	"skal": true, "sådan": true, "under": true, "uden": true, "vores": true,
	"være": true, "deres": true, "altid": true, "eget": true,
	// English
	"about": true, "after": true, "also": true, "been": true, "before": true,
	"could": true, "does": true, "each": true, "every": true, "from": true,
	"into": true, "just": true, "make": true, "more": true, "must": true,
	"only": true, "over": true, "same": true, "should": true, "some": true,
	"than": true, "that": true, "them": true, "then": true, "there": true,
	"these": true, "they": true, "this": true, "those": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "will": true,
	"with": true, "would": true, "your": true,
}

// SearchTerms turns an issue's text into the keywords the candidate query
// scores on. The title carries the signal; the description is only mined when
// the title alone yields too little.
//
// Output is lowercase, deduplicated, letters and digits only — no LIKE
// metacharacters can survive, which is what makes the ILIKE query safe.
func SearchTerms(title, description string) []string {
	terms := tokenize(title)
	if len(terms) < 3 {
		terms = append(terms, tokenize(firstLines(description, 3))...)
	}
	return dedupe(terms, maxTerms)
}

func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len([]rune(f)) < minTermLen || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// dedupe keeps first-seen order (title words before description words) and
// then caps to the longest terms, which are the most discriminating.
func dedupe(terms []string, limit int) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(terms))
	for _, t := range terms {
		if seen[t] {
			continue
		}
		seen[t] = true
		unique = append(unique, t)
	}
	if len(unique) <= limit {
		return unique
	}
	ranked := make([]string, len(unique))
	copy(ranked, unique)
	sort.SliceStable(ranked, func(a, b int) bool {
		return len([]rune(ranked[a])) > len([]rune(ranked[b]))
	})
	kept := map[string]bool{}
	for _, t := range ranked[:limit] {
		kept[t] = true
	}
	out := make([]string, 0, limit)
	for _, t := range unique {
		if kept[t] {
			out = append(out, t)
		}
	}
	return out
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ")
}
