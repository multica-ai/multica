package handler

import (
	"fmt"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// quickActionTarget is the resolved execution target of an action: the agent
// that will actually run, plus the display identity the client should show.
// For a squad binding, Agent is the squad leader and Name is the SQUAD's name,
// because the squad is what the user bound and what the mention will address.
type quickActionTarget struct {
	Agent     db.Agent
	Name      string
	AvatarURL string
	// MentionType is the mention:// scheme the rendered comment must use so
	// the existing trigger path routes it exactly as a hand-typed mention.
	MentionType string
	// MentionID is the id inside the mention link: the agent id, or the SQUAD
	// id for a squad binding (the trigger path resolves the leader itself).
	MentionID string
	Found     bool
}

// buildQuickActionBody prepends the mention line that actually triggers the
// run. It is parsed by the same ParseMentions the composer's output goes
// through, so a quick action is indistinguishable from a hand-typed @mention
// downstream. The prompt itself is passed through untouched.
func buildQuickActionBody(qa db.QuickAction, target quickActionTarget) string {
	if qa.AssigneeType == "" {
		return qa.Prompt
	}
	return fmt.Sprintf("[@%s](mention://%s/%s)\n\n%s", target.Name, target.MentionType, target.MentionID, qa.Prompt)
}
