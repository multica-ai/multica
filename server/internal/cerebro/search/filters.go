// Package search implements FIR-2595 search trin 1: Postgres-FTS-backed issue
// search with Slack-style inline filters. Lives in the cerebro zone so the
// upstream issue.go handler only needs a tiny CEREBRO-PATCH redirect.
package search

import (
	"strings"
	"unicode"
)

// FilterKey is the recognised inline-filter prefix. Unknown prefixes are left
// in the free-text part of the query so a `foo:bar` typo still searches.
type FilterKey string

const (
	FilterFrom     FilterKey = "from"
	FilterAssignee FilterKey = "assignee"
	FilterStatus   FilterKey = "status"
	FilterProject  FilterKey = "project"
	FilterHas      FilterKey = "has"
)

// knownFilters is the closed set of inline-filter prefixes that get extracted
// from the raw query. Anything else (e.g. "url:https://...") stays in the
// free-text portion so it can still match an issue body.
var knownFilters = map[string]FilterKey{
	"from":     FilterFrom,
	"assignee": FilterAssignee,
	"status":   FilterStatus,
	"project":  FilterProject,
	"has":      FilterHas,
}

// Filter is a single key:value pair parsed out of the raw query, e.g.
// `from:jesper` → {Key: "from", Value: "jesper"}.
type Filter struct {
	Key   FilterKey
	Value string
}

// Parsed is the result of running Parse over a raw search string.
type Parsed struct {
	// Text is the free-text portion that should drive the FTS query.
	Text string
	// Filters lists the inline filters in the order they appeared. The
	// handler resolves each one against the workspace (member, agent, project
	// or status enum) before building the SQL.
	Filters []Filter
}

// HasFilter returns true when at least one filter for the given key was
// supplied. Used by the handler to decide whether to JOIN extra tables.
func (p Parsed) HasFilter(key FilterKey) bool {
	for _, f := range p.Filters {
		if f.Key == key {
			return true
		}
	}
	return false
}

// Values collects every value supplied for a given key. The handler treats
// repeated keys as OR (e.g. `from:jesper from:mia` → from = jesper OR mia).
func (p Parsed) Values(key FilterKey) []string {
	out := make([]string, 0, len(p.Filters))
	for _, f := range p.Filters {
		if f.Key == key {
			out = append(out, f.Value)
		}
	}
	return out
}

// Parse extracts inline filters from a raw search string. Supports quoted
// values for filters whose value contains spaces (e.g. `project:"my project"`).
// Unknown prefixes are kept in the free-text portion verbatim so a typo or a
// real `something:foo` substring still searches.
//
// Examples:
//
//	Parse("login bug from:jesper status:todo")
//	  → Text="login bug", Filters=[{from,jesper},{status,todo}]
//	Parse(`project:"lager" deploy`)
//	  → Text="deploy", Filters=[{project,lager}]
//	Parse("has:attachment")
//	  → Text="",        Filters=[{has,attachment}]
func Parse(raw string) Parsed {
	var (
		filters []Filter
		text    strings.Builder
	)

	runes := []rune(raw)
	n := len(runes)
	i := 0
	emit := func(s string) {
		if s == "" {
			return
		}
		if text.Len() > 0 {
			text.WriteRune(' ')
		}
		text.WriteString(s)
	}

	for i < n {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}

		// Peek ahead for a `key:` prefix at the current position.
		keyEnd := -1
		for j := i; j < n && !unicode.IsSpace(runes[j]); j++ {
			if runes[j] == ':' {
				keyEnd = j
				break
			}
		}
		if keyEnd > i {
			keyStr := strings.ToLower(string(runes[i:keyEnd]))
			if key, known := knownFilters[keyStr]; known {
				valStart := keyEnd + 1
				var value string
				switch {
				case valStart < n && (runes[valStart] == '"' || runes[valStart] == '\''):
					// Quoted value — scan to matching quote, allowing spaces.
					quote := runes[valStart]
					end := valStart + 1
					for end < n && runes[end] != quote {
						end++
					}
					value = string(runes[valStart+1 : end])
					if end < n {
						end++ // consume closing quote
					}
					i = end
				default:
					end := valStart
					for end < n && !unicode.IsSpace(runes[end]) {
						end++
					}
					value = string(runes[valStart:end])
					i = end
				}
				if v := strings.TrimSpace(value); v != "" {
					filters = append(filters, Filter{Key: key, Value: v})
				}
				continue
			}
		}

		// Plain word — copy to free-text buffer.
		start := i
		for i < n && !unicode.IsSpace(runes[i]) {
			i++
		}
		emit(string(runes[start:i]))
	}

	return Parsed{
		Text:    strings.TrimSpace(text.String()),
		Filters: filters,
	}
}

// StatusMap normalises a status filter value (case-insensitive) into the
// concrete db enum values to match. Returns nil if the value is unrecognised
// so the handler can surface "no results" cleanly instead of erroring.
//
// Aliases:
//   - "open"   → todo, in_progress, in_review, blocked, backlog
//   - "closed" → done, cancelled
//   - "active" → in_progress, in_review
func StatusMap(value string) []string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "open":
		return []string{"todo", "in_progress", "in_review", "blocked", "backlog"}
	case "closed":
		return []string{"done", "cancelled"}
	case "active":
		return []string{"in_progress", "in_review"}
	case "todo":
		return []string{"todo"}
	case "in_progress", "in-progress", "inprogress", "doing":
		return []string{"in_progress"}
	case "in_review", "in-review", "review":
		return []string{"in_review"}
	case "blocked":
		return []string{"blocked"}
	case "backlog":
		return []string{"backlog"}
	case "done":
		return []string{"done"}
	case "cancelled", "canceled":
		return []string{"cancelled"}
	default:
		return nil
	}
}
