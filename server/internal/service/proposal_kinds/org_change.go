package proposalkinds

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// KeyOrgChange is the kind key of an organisation change (spec 4.6). Its
// payload.type selects one of eight changes. The kind never auto-approves:
// the standing approvals engine refuses it in code.
const KeyOrgChange = "org_change"

// OrgChanges is what the org_change kind needs from the service that knows the
// eight changes. Validate runs at filing, outside any transaction. Apply runs
// inside the approval transaction and records the application.
type OrgChanges interface {
	// ValidateOrgChange checks a payload and returns its canonical form and a
	// one-line summary for lists.
	ValidateOrgChange(ctx context.Context, q *db.Queries, ws pgtype.UUID, payload []byte) (canonical []byte, summary string, err error)
	// ApplyOrgChange applies the change, runs in the decision transaction, and
	// writes the application row and its governance event.
	ApplyOrgChange(ctx context.Context, q *db.Queries, d Decision) error
}

// NewOrgChangeKind returns the org_change handler over changes.
func NewOrgChangeKind(changes OrgChanges) Kind {
	return orgChangeKind{changes: changes}
}

type orgChangeKind struct {
	changes OrgChanges
}

func (orgChangeKind) Key() string         { return KeyOrgChange }
func (orgChangeKind) FiledByAgents() bool { return true }

func (k orgChangeKind) Validate(ctx context.Context, env Env, payload []byte) ([]byte, error) {
	canonical, _, err := k.changes.ValidateOrgChange(ctx, env.Q, env.WorkspaceID, payload)
	return canonical, err
}

// Summarize reads the change's type and the one-line summary the payload
// carries; a payload that does not parse reads as empty.
func (orgChangeKind) Summarize(payload []byte) string {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &head) != nil {
		return ""
	}
	return OrgChangeTypeLabel(head.Type)
}

// OrgChangeTypeLabel is the short name of an organisation change type, for lists.
func OrgChangeTypeLabel(t string) string {
	switch t {
	case "instruction_change":
		return "Change an agent's instructions"
	case "skill_change":
		return "Change an agent's skills"
	case "charter_change":
		return "Change a squad's charter"
	case "autonomy_change":
		return "Change a squad's autonomy level"
	case "hire":
		return "Hire an agent"
	case "retire":
		return "Retire an agent or squad"
	case "move":
		return "Move an agent to another squad"
	case "merge_squads":
		return "Merge two squads"
	case "unretire":
		return "Restore an archived agent or squad"
	case "unmerge":
		return "Unmerge two squads"
	}
	return fmt.Sprintf("Organisation change (%s)", t)
}

func (k orgChangeKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	return Effect{}, k.changes.ApplyOrgChange(ctx, q, d)
}

// Reject has nothing to undo: a rejected change was never applied.
func (orgChangeKind) Reject(context.Context, *db.Queries, Decision) error { return nil }
