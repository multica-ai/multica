package inbox

import (
	"context"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Delivery is how much of an issue one member's subscription to it delivers.
// The notification fanout narrows by it, and the subscriber list reports it so
// clients show the tier without re-deriving these rules (MUL-9360).
type Delivery string

const (
	// DeliveryAll delivers every event.
	DeliveryAll Delivery = "all"
	// DeliveryKeyUpdates is the bystander tier on someone else's issue:
	// mentions, replies in the member's threads, and handoff statuses.
	DeliveryKeyUpdates Delivery = "key_updates"
	// DeliveryDelegated is the tier of an issue an agent created on the
	// member's behalf: mentions, comments, failures and handoff statuses.
	DeliveryDelegated Delivery = "delegated"
)

// OthersIssuesPref is the preference key for how much of SOMEONE ELSE'S issue
// reaches a member who is only passively subscribed to it. Absent or
// "key_updates" applies the bystander tier; "all" turns it off (MUL-9343).
const OthersIssuesPref = "others_issues"

// bystanderReasons are the subscription reasons a member picks up as a side
// effect of acting on an issue — filing it, once being its assignee,
// commenting, being mentioned. On an issue assigned to someone else these
// narrow to the bystander tier. manual and autopilot are explicit requests to
// follow the issue and keep every event; delegated has its own tier.
var bystanderReasons = map[string]bool{
	"creator":   true,
	"assignee":  true,
	"commenter": true,
	"mentioned": true,
}

// SubscriptionDelivery is the tier a member's direct subscription row delivers.
// prefs are the member's parsed notification preferences. isSomeoneElses is
// only called for a passive reason the member has not widened to "all", so a
// fanout reads the issue's owner only when it matters.
//
// A project subscription delivers every event through its own fanout, so a
// member who also watches the issue's project receives DeliveryAll whatever
// this returns; callers reporting the effective tier apply that on top.
func SubscriptionDelivery(reason string, prefs map[string]string, isSomeoneElses func() bool) Delivery {
	if reason == "delegated" {
		return DeliveryDelegated
	}
	if !bystanderReasons[reason] || prefs[OthersIssuesPref] == "all" {
		return DeliveryAll
	}
	if !isSomeoneElses() {
		return DeliveryAll
	}
	return DeliveryKeyUpdates
}

// IssueOwner is who an issue is assigned to, resolved to the human behind it:
// the assigned member, or the owner of the assigned agent. A squad assignee
// has no single owner, so a squad's issue is nobody's own.
type IssueOwner struct {
	assigned bool
	ownerID  string
}

// IsSomeoneElses reports whether the issue is assigned, and not to userID or
// an agent userID owns. An unassigned issue is nobody else's: its creator is
// the closest thing it has to an owner, and nothing about it is routine yet.
func (o IssueOwner) IsSomeoneElses(userID string) bool {
	return o.assigned && o.ownerID != userID
}

// LoadIssueOwner resolves the owner of issue, reading the assigned agent when
// there is one.
func LoadIssueOwner(ctx context.Context, queries *db.Queries, issue db.Issue) (IssueOwner, error) {
	if !issue.AssigneeID.Valid {
		return IssueOwner{}, nil
	}
	switch issue.AssigneeType.String {
	case "member":
		return IssueOwner{assigned: true, ownerID: util.UUIDToString(issue.AssigneeID)}, nil
	case "agent":
		agent, err := queries.GetAgent(ctx, issue.AssigneeID)
		if err != nil {
			return IssueOwner{}, err
		}
		owner := ""
		if agent.OwnerID.Valid {
			owner = util.UUIDToString(agent.OwnerID)
		}
		return IssueOwner{assigned: true, ownerID: owner}, nil
	default:
		return IssueOwner{assigned: true}, nil
	}
}
