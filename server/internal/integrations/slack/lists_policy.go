package slack

import (
	"os"
	"strings"
)

// Default Daykee Pixie command targets. Operators can override with
// MULTICA_SLACK_LISTS_IDEA_LIST_ID / MULTICA_SLACK_LISTS_FEATURE_LIST_ID.
// These IDs are the /idea and /feature write mapping only — the security
// allowlist lives on the Slack installation (lists_allowlist, fail-closed).
const (
	defaultIdeaListID    = "F0BR8PBUAQH"
	defaultFeatureListID = "F0BRAH9R068"
)

// ListsCommand is a user-authorized Lists write intent parsed from the latest
// inbound chat text. Ordinary chat (no /idea or /feature prefix) is not a
// write command.
type ListsCommand string

const (
	ListsCommandNone    ListsCommand = ""
	ListsCommandIdea    ListsCommand = "idea"
	ListsCommandFeature ListsCommand = "feature"
)

// ListsPolicy is the server-side command→list mapping. A write must name the
// mapped list AND the latest user message must carry the matching command.
type ListsPolicy struct {
	IdeaListID    string
	FeatureListID string
}

// LoadListsPolicy reads the deployment mapping. Empty env keeps the Daykee
// defaults so a self-host that already created those two Lists works without
// a second config pass. An explicit empty override (value "-") disables that
// command's list.
func LoadListsPolicy() ListsPolicy {
	return listsPolicyFromEnv(os.Getenv)
}

func listsPolicyFromEnv(getenv func(string) string) ListsPolicy {
	idea := firstNonEmpty(getenv("MULTICA_SLACK_LISTS_IDEA_LIST_ID"), defaultIdeaListID)
	feature := firstNonEmpty(getenv("MULTICA_SLACK_LISTS_FEATURE_LIST_ID"), defaultFeatureListID)
	if idea == "-" {
		idea = ""
	}
	if feature == "-" {
		feature = ""
	}
	return ListsPolicy{
		IdeaListID:    strings.TrimSpace(idea),
		FeatureListID: strings.TrimSpace(feature),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ListIDFor returns the mapped list for a write command, or "".
func (p ListsPolicy) ListIDFor(cmd ListsCommand) string {
	switch cmd {
	case ListsCommandIdea:
		return p.IdeaListID
	case ListsCommandFeature:
		return p.FeatureListID
	default:
		return ""
	}
}

// ParseListsCommand reads a mention-stripped user message. Only a leading
// /idea or /feature (ASCII, case-insensitive) counts as authorization to write.
// /ideal and similar prefixes do not match.
func ParseListsCommand(text string) (ListsCommand, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ListsCommandNone, ""
	}
	if ok, rest := commandPrefix(trimmed, "/idea"); ok {
		return ListsCommandIdea, rest
	}
	if ok, rest := commandPrefix(trimmed, "/feature"); ok {
		return ListsCommandFeature, rest
	}
	return ListsCommandNone, trimmed
}

func commandPrefix(text, cmd string) (bool, string) {
	if !strings.HasPrefix(strings.ToLower(text), cmd) {
		return false, ""
	}
	rest := text[len(cmd):]
	if rest == "" {
		return true, ""
	}
	switch rest[0] {
	case ' ', '\t', '\n', '\r':
		return true, strings.TrimSpace(rest)
	default:
		return false, ""
	}
}

// listIDAllowed is an exact, case-sensitive allowlist check. Empty allowlist
// and prefix matches fail closed.
func listIDAllowed(allow []string, listID string) bool {
	listID = strings.TrimSpace(listID)
	if listID == "" {
		return false
	}
	for _, id := range allow {
		if id == listID {
			return true
		}
	}
	return false
}
