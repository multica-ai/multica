package github

import (
	"sort"
	"strings"
)

// This file encodes the katalon-studio/product "ways-of-work" workflow on
// top of the raw GitHub Projects v2 board:
//
//   - Status   (Backlog/To Do/In Progress/In Review/Approved/Done/Wont do)
//     <-> Multica's fixed status enum.
//   - Priority (P0..P3) <-> Multica's priority enum.
//   - Area / Pod / Intent Ref / type:* are carried onto the Multica issue
//     as labels + metadata so the board reflects the same taxonomy the
//     product repo defines (pod-routing.md, area-taxonomy.md, labels.md).
//   - Parent issue / sub-issue links become Multica parent_issue_id, which
//     is how parent-features-and-sub-tasks.md models Intents -> tasks.

// --- Status -------------------------------------------------------------

// statusGitHubToMultica maps a GitHub board Status option to the Multica
// status enum. "Approved" has no Multica equivalent; it folds into
// in_review (the raw value is preserved in metadata for lossless push-back).
var statusGitHubToMultica = map[string]string{
	"Backlog":     "backlog",
	"To Do":       "todo",
	"In Progress": "in_progress",
	"In Review":   "in_review",
	"Approved":    "in_review",
	"Done":        "done",
	"Wont do":     "cancelled",
}

// statusMulticaToGitHub maps a Multica status to the canonical GitHub board
// option. blocked has no board option and stays as In Progress on the board
// (the blocked signal lives on the Multica side / as a label).
var statusMulticaToGitHub = map[string]string{
	"backlog":     "Backlog",
	"todo":        "To Do",
	"in_progress": "In Progress",
	"in_review":   "In Review",
	"done":        "Done",
	"blocked":     "In Progress",
	"cancelled":   "Wont do",
}

// MapStatusToMultica returns the Multica status for a GitHub Status option,
// defaulting to backlog when the option is empty/unknown (enum drift
// downgrades, never crashes — per the repo's API-compat rules).
func MapStatusToMultica(ghStatus string) string {
	if s, ok := statusGitHubToMultica[strings.TrimSpace(ghStatus)]; ok {
		return s
	}
	return "backlog"
}

// MapStatusToGitHub returns the GitHub board option for a Multica status.
func MapStatusToGitHub(multicaStatus string) (string, bool) {
	s, ok := statusMulticaToGitHub[multicaStatus]
	return s, ok
}

// --- Priority -----------------------------------------------------------

var priorityGitHubToMultica = map[string]string{
	"P0": "urgent",
	"P1": "high",
	"P2": "medium",
	"P3": "low",
}

var priorityMulticaToGitHub = map[string]string{
	"urgent": "P0",
	"high":   "P1",
	"medium": "P2",
	"low":    "P3",
}

// MapPriorityToMultica defaults to "none" when unset/unknown.
func MapPriorityToMultica(ghPriority string) string {
	if p, ok := priorityGitHubToMultica[strings.TrimSpace(ghPriority)]; ok {
		return p
	}
	return "none"
}

// MapPriorityToGitHub returns the board option for a Multica priority.
func MapPriorityToGitHub(multicaPriority string) (string, bool) {
	p, ok := priorityMulticaToGitHub[multicaPriority]
	return p, ok
}

// --- Labels + metadata --------------------------------------------------

// DeriveLabels builds the set of Multica labels that should mirror the
// product-repo taxonomy for an item: its existing repo labels (area:*,
// pod:*, type:*, lane:*, etc.) plus board-field-derived area:/pod: labels
// so the board groups consistently even when the repo label is missing.
func DeriveLabels(it Item) []string {
	set := map[string]struct{}{}
	for _, l := range it.Labels {
		l = strings.TrimSpace(l)
		if l != "" {
			set[l] = struct{}{}
		}
	}
	if area := strings.TrimSpace(it.Fields["Area"]); area != "" {
		set["area:"+strings.ToLower(area)] = struct{}{}
	}
	if pod := strings.TrimSpace(it.Fields["Pod"]); pod != "" {
		set["pod:"+strings.ToLower(pod)] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// PodFromItem returns the normalized pod (board Pod field, falling back to
// a pod:* repo label). Empty when the item has no pod — pod-routing.md
// treats those as unrouted.
func PodFromItem(it Item) string {
	if pod := strings.TrimSpace(it.Fields["Pod"]); pod != "" {
		return pod
	}
	for _, l := range it.Labels {
		if rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(l)), "pod:"); ok {
			return canonicalPod(rest)
		}
	}
	return ""
}

// canonicalPod normalizes a pod label slug to its board display name.
func canonicalPod(slug string) string {
	switch strings.ToLower(slug) {
	case "dlt":
		return "DLT"
	case "agentic":
		return "Agentic"
	case "mht":
		return "MHT"
	case "truetest":
		return "TrueTest"
	case "studio":
		return "Studio"
	default:
		return ""
	}
}

// IssueType returns the type:* label value (intent/task/bug/story/...),
// which parent-features-and-sub-tasks.md uses to distinguish Intents
// (parents) from downstream work. Falls back to a [bracket] title prefix.
func IssueType(it Item) string {
	for _, l := range it.Labels {
		if rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(l)), "type:"); ok {
			return rest
		}
	}
	t := strings.ToLower(it.Title)
	switch {
	case strings.HasPrefix(t, "[intent]"):
		return "intent"
	case strings.HasPrefix(t, "[bug]"):
		return "bug"
	case strings.HasPrefix(t, "[task]"):
		return "task"
	case strings.HasPrefix(t, "[story]"):
		return "story"
	}
	return ""
}

// MetadataFor returns the issue.metadata key/values that carry the GitHub
// + workflow identity onto the Multica issue. Keys match the metadata
// contract (^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$, primitive values).
func MetadataFor(it Item) map[string]string {
	m := map[string]string{
		"gh_item_id": it.ItemID,
	}
	if it.Repo != "" {
		m["gh_repo"] = it.Repo
	}
	if it.Number > 0 {
		m["gh_number"] = itoa(it.Number)
	}
	if it.URL != "" {
		m["gh_url"] = it.URL
	}
	if s := strings.TrimSpace(it.Fields["Status"]); s != "" {
		m["gh_status"] = s
	}
	if area := strings.TrimSpace(it.Fields["Area"]); area != "" {
		m["area"] = strings.ToLower(area)
	}
	if pod := PodFromItem(it); pod != "" {
		m["pod"] = pod
	}
	if t := IssueType(it); t != "" {
		m["issue_type"] = t
	}
	if ref := strings.TrimSpace(it.Fields["Intent Ref"]); ref != "" {
		m["intent_ref"] = ref
	}
	if td := strings.TrimSpace(it.Fields["Target date"]); td != "" {
		m["target_date"] = td
	}
	return m
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
