// Package issuesync owns the transport-independent model for mirroring issues
// between Multica and an external tracker (GitLab today, Jira next).
//
// Nothing here touches the network or the database, so the whole contract is
// testable without a live tracker. Three decisions carry the model, and each
// one exists because the obvious alternative fails on real data:
//
//   - Links live in the descriptions, not in a mapping table. Each side carries
//     a hidden HTML comment naming its counterpart, so a pair survives a lost
//     database, a fresh checkout, and a second machine, and needs no migration.
//     Both directions are read, so a marker that survived on only one side
//     still re-links the pair.
//   - Comparison is content-first. Two issues are compared by what they say,
//     not by when they were touched. Comparing timestamps first ping-pongs:
//     writing an issue bumps its own updated_at, so each side takes turns
//     believing it is newer and overwrites the other forever.
//   - A field the tracker cannot store is excluded from the comparison rather
//     than written and hoped for. GitLab CE silently drops start_date, so
//     including it leaves every issue that has one permanently "changed" and
//     rewritten on every run.
package issuesync

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// Side names one end of a link. The value doubles as the HTML-comment tag that
// carries that side's marker, so "gitlab" in a Multica description and
// "multica" in a GitLab description are one vocabulary rather than two.
type Side string

const (
	SideMultica Side = "multica"
	SideGitLab  Side = "gitlab"
)

// markerTags is the closed set of tags scanMarkers recognizes. An HTML comment
// with any other tag is ordinary description text and is left alone.
var markerTags = []string{string(SideMultica), string(SideGitLab)}

// Marker is the counterpart identity embedded in an issue description. Only the
// fields that side needs are set: a Multica description carries the tracker's
// project and issue number, a tracker description carries the Multica
// identifier and UUID.
//
// The JSON is flat on purpose — scanMarkers finds the comment by its closing
// delimiter rather than by brace matching, but a flat object keeps the marker
// readable in a diff and stable under json.Marshal's field order.
type Marker struct {
	Project    int64  `json:"project,omitempty"`
	IID        int64  `json:"iid,omitempty"`
	Identifier string `json:"identifier,omitempty"`
	ID         string `json:"id,omitempty"`
}

// Empty reports whether the marker names no counterpart. A marker that parses
// but names nothing is treated as absent, so a half-written comment cannot
// claim a link.
func (m Marker) Empty() bool {
	return m.Project == 0 && m.IID == 0 && m.Identifier == "" && m.ID == ""
}

// markerSpan is one recognized marker comment and its byte range in the body.
type markerSpan struct {
	start, end int
	tag        string
	payload    string
}

// scanMarkers returns every recognized marker comment in body, in order.
//
// The scan is delimiter-driven: it looks for "<!--", then for the next "-->",
// and only then asks whether the text between them is a marker. That keeps a
// description containing an ordinary HTML comment, or a marker-shaped string
// inside a code block, from being mistaken for a link.
func scanMarkers(body string) []markerSpan {
	var spans []markerSpan
	for i := 0; i < len(body); {
		open := strings.Index(body[i:], "<!--")
		if open < 0 {
			return spans
		}
		open += i
		closing := strings.Index(body[open:], "-->")
		if closing < 0 {
			return spans
		}
		closing += open
		end := closing + len("-->")
		if tag, payload, ok := splitMarker(body[open+len("<!--") : closing]); ok {
			spans = append(spans, markerSpan{start: open, end: end, tag: tag, payload: payload})
		}
		i = end
	}
	return spans
}

// splitMarker splits the inside of a comment into its tag and JSON payload.
func splitMarker(inner string) (tag, payload string, ok bool) {
	colon := strings.Index(inner, ":")
	if colon < 0 {
		return "", "", false
	}
	tag = strings.TrimSpace(inner[:colon])
	if !knownTag(tag) {
		return "", "", false
	}
	payload = strings.TrimSpace(inner[colon+1:])
	if !strings.HasPrefix(payload, "{") {
		return "", "", false
	}
	return tag, payload, true
}

func knownTag(tag string) bool {
	for _, known := range markerTags {
		if tag == known {
			return true
		}
	}
	return false
}

// ParseMarker returns the marker side carries in body. A comment with a
// different tag, malformed JSON, or no fields at all is not a marker.
func ParseMarker(body string, side Side) (Marker, bool) {
	for _, span := range scanMarkers(body) {
		if span.tag != string(side) {
			continue
		}
		var m Marker
		if err := json.Unmarshal([]byte(span.payload), &m); err != nil {
			continue
		}
		if m.Empty() {
			continue
		}
		return m, true
	}
	return Marker{}, false
}

// SetMarker returns body with side's marker set to m, replacing any marker that
// side already had.
//
// The result is byte-identical when the marker is already the one asked for, so
// a run that decides nothing changed writes nothing. Replacing rather than
// appending is what keeps a re-link from stacking comments in the description.
func SetMarker(body string, side Side, m Marker) string {
	payload, err := json.Marshal(m)
	if err != nil {
		// Marker holds only strings and integers, so Marshal cannot fail. The
		// branch exists so the signature stays free of an error no caller could
		// act on.
		return body
	}
	cleaned := strings.TrimRight(stripTag(body, side), "\n")
	comment := "<!-- " + string(side) + ": " + string(payload) + " -->"
	if strings.TrimSpace(cleaned) == "" {
		return comment
	}
	return cleaned + "\n\n" + comment
}

// StripMarkers returns body without any link marker, from either side.
func StripMarkers(body string) string {
	var b strings.Builder
	last := 0
	for _, span := range scanMarkers(body) {
		b.WriteString(body[last:span.start])
		last = span.end
	}
	b.WriteString(body[last:])
	return b.String()
}

// stripTag returns body without the markers of one side, leaving the other
// side's marker in place.
func stripTag(body string, side Side) string {
	var b strings.Builder
	last := 0
	for _, span := range scanMarkers(body) {
		if span.tag != string(side) {
			continue
		}
		b.WriteString(body[last:span.start])
		last = span.end
	}
	b.WriteString(body[last:])
	return b.String()
}

// Field names reported by Diff, in the order Diff reports them.
const (
	FieldTitle       = "title"
	FieldDescription = "description"
	FieldStatus      = "status"
	FieldPriority    = "priority"
	FieldDueDate     = "due_date"
)

// ComparedFields returns the fields Diff can report, in a stable order.
//
// A field the tracker cannot store is absent from this list and from Issue
// entirely. start_date is the one that bit us: GitLab CE accepts it, drops it,
// and returns nothing, so a comparison that included it would report the same
// issue as changed on every run forever.
func ComparedFields() []string {
	return []string{FieldTitle, FieldDescription, FieldStatus, FieldPriority, FieldDueDate}
}

// Issue is the normalized shape of one issue on one side.
//
// Assignees, comments, attachments and checklists are absent on purpose: they
// are out of scope for the mirror, and carrying them here would imply a
// comparison the model does not make.
type Issue struct {
	Side Side
	// Key is the issue's identity on its own side: the Multica identifier
	// ("PROJECT-1") or the tracker issue number as text.
	Key       string
	Title     string
	Body      string // description, link markers included
	Status    string // canonical status key
	Priority  string // canonical priority key; "" when the side has no such field
	DueDate   string // "2006-01-02"; "" when unset
	UpdatedAt time.Time
}

// Diff reports the fields whose content differs, in ComparedFields order. An
// empty result means the two issues agree and neither side needs a write.
//
// Priority is compared only when both sides carry one. A tracker without a
// priority field reports "", and comparing that against a real value would
// report a difference no write could ever clear.
func Diff(a, b Issue) []string {
	var fields []string
	if strings.TrimSpace(a.Title) != strings.TrimSpace(b.Title) {
		fields = append(fields, FieldTitle)
	}
	if normalizeBody(a.Body) != normalizeBody(b.Body) {
		fields = append(fields, FieldDescription)
	}
	if a.Status != b.Status {
		fields = append(fields, FieldStatus)
	}
	if a.Priority != "" && b.Priority != "" && a.Priority != b.Priority {
		fields = append(fields, FieldPriority)
	}
	if a.DueDate != b.DueDate {
		fields = append(fields, FieldDueDate)
	}
	return fields
}

// normalizeBody reduces a description to the text the comparison cares about:
// markers removed, line endings unified, surrounding whitespace dropped. A
// description edited in a web form comes back with CRLF and a trailing newline
// that the other side never had; without this, every such edit would look like
// a content change.
func normalizeBody(body string) string {
	body = StripMarkers(body)
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.TrimSpace(body)
}

// DecisionKind is what a run should do with one linked pair.
type DecisionKind int

const (
	// NoChange means the pair agrees; nothing is written.
	NoChange DecisionKind = iota
	// UpdateFrom means the pair disagrees and Source is the newer side, whose
	// content should be copied to the other.
	UpdateFrom
	// Conflict means the pair disagrees and neither side is newer. A run
	// reports it instead of picking a winner, because picking one silently is
	// how a mirror loses an edit.
	Conflict
)

// Decision is the outcome for one linked pair.
type Decision struct {
	Kind   DecisionKind
	Source Side     // set for UpdateFrom
	Fields []string // fields that differ; empty for NoChange
}

// Changed reports whether the decision requires a write.
func (d Decision) Changed() bool { return d.Kind == UpdateFrom }

// Decide compares a linked pair and says what to do.
//
// Content decides first and timestamps only break a real tie. That order is the
// whole point: if timestamps decided first, the write that resolves a
// difference would bump the writer's updated_at and hand the next run to the
// other side, which would then write back. The pair would never settle.
func Decide(a, b Issue) Decision {
	fields := Diff(a, b)
	if len(fields) == 0 {
		return Decision{Kind: NoChange}
	}
	switch {
	case a.UpdatedAt.After(b.UpdatedAt):
		return Decision{Kind: UpdateFrom, Source: a.Side, Fields: fields}
	case b.UpdatedAt.After(a.UpdatedAt):
		return Decision{Kind: UpdateFrom, Source: b.Side, Fields: fields}
	default:
		return Decision{Kind: Conflict, Fields: fields}
	}
}

// Pair is one linked Multica issue and tracker issue.
type Pair struct {
	Multica Issue
	Tracker Issue
}

// LinkResult is the outcome of matching two sides.
type LinkResult struct {
	Pairs []Pair
	// MulticaOnly and TrackerOnly hold issues no marker linked. A run creates
	// the missing counterpart for these; they are returned rather than created
	// here so the caller owns every write.
	MulticaOnly []Issue
	TrackerOnly []Issue
}

// Link matches issues from both sides using the markers in their descriptions.
//
// Two passes, in this order, so a pair whose marker survived on only one side
// still re-links: first every tracker issue is read for a Multica marker, then
// every still-unpaired Multica issue is read for a tracker marker. A single
// pass would strand exactly the pairs a human edited by hand.
//
// A run is scoped to one tracker project, so tracker issues are matched by
// number alone. Matching across projects would need the project id in the key
// and is not what the marker's Project field is for.
func Link(multica, tracker []Issue) LinkResult {
	byIdentifier := make(map[string]int, len(multica))
	byID := make(map[string]int, len(multica))
	for i, issue := range multica {
		if issue.Key != "" {
			byIdentifier[issue.Key] = i
		}
		if m, ok := ParseMarker(issue.Body, SideGitLab); ok && m.ID != "" {
			byID[m.ID] = i
		}
	}

	pairedMultica := make([]bool, len(multica))
	pairedTracker := make([]bool, len(tracker))
	var result LinkResult

	// Pass 1: the tracker side names its Multica counterpart.
	for ti, issue := range tracker {
		m, ok := ParseMarker(issue.Body, SideMultica)
		if !ok {
			continue
		}
		mi, found := byIdentifier[m.Identifier]
		if !found && m.ID != "" {
			mi, found = byID[m.ID]
		}
		if !found || pairedMultica[mi] {
			continue
		}
		pairedMultica[mi] = true
		pairedTracker[ti] = true
		result.Pairs = append(result.Pairs, Pair{Multica: multica[mi], Tracker: issue})
	}

	// Pass 2: the Multica side names its tracker counterpart.
	byNumber := make(map[string]int, len(tracker))
	for ti, issue := range tracker {
		if !pairedTracker[ti] && issue.Key != "" {
			byNumber[issue.Key] = ti
		}
	}
	for mi, issue := range multica {
		if pairedMultica[mi] {
			continue
		}
		m, ok := ParseMarker(issue.Body, SideGitLab)
		if !ok {
			continue
		}
		ti, found := byNumber[strconv.FormatInt(m.IID, 10)]
		if !found || pairedTracker[ti] {
			continue
		}
		pairedMultica[mi] = true
		pairedTracker[ti] = true
		result.Pairs = append(result.Pairs, Pair{Multica: issue, Tracker: tracker[ti]})
	}

	for mi, issue := range multica {
		if !pairedMultica[mi] {
			result.MulticaOnly = append(result.MulticaOnly, issue)
		}
	}
	for ti, issue := range tracker {
		if !pairedTracker[ti] {
			result.TrackerOnly = append(result.TrackerOnly, issue)
		}
	}
	return result
}

// StatusLabelPrefix marks the tracker labels that carry a status.
const StatusLabelPrefix = "status::"

// StatusLabel returns the tracker label that carries a canonical status key.
//
// The label alphabet uses "-" where the stored key uses "_", because the label
// is typed by hand in the tracker's UI and "status::in-review" reads as one
// word where "status::in_review" reads as two.
func StatusLabel(status string) (string, bool) {
	if !issuestatus.IsBuiltIn(status) {
		return "", false
	}
	return StatusLabelPrefix + strings.ReplaceAll(status, "_", "-"), true
}

// StatusFromLabels returns the canonical status key carried by labels. The
// first recognized label wins; a label naming an unknown status is ignored
// rather than guessed at.
func StatusFromLabels(labels []string) (string, bool) {
	for _, label := range labels {
		rest, ok := strings.CutPrefix(label, StatusLabelPrefix)
		if !ok {
			continue
		}
		status := strings.ReplaceAll(rest, "-", "_")
		if issuestatus.IsBuiltIn(status) {
			return status, true
		}
	}
	return "", false
}

// IsClosing reports whether a status closes the issue on the tracker side.
// Done and cancelled both close; everything else reopens, so a status moved
// back out of a terminal state is not left closed.
func IsClosing(status string) bool {
	return status == issuestatus.Done || status == issuestatus.Cancelled
}
