package handler

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// agentBuilderSystemKeyPrefix marks the hidden execution carrier behind an
// unfinished AI agent-creation flow. The suffix is the flow id.
const agentBuilderSystemKeyPrefix = "agent_builder:"

// A runtime or profile delete is refused while agents are still bound. Telling
// the user to "reassign or archive them" is only true for agents they can
// actually reach: the archive endpoint rejects anything carrying a system_key
// outright, and a builder carrier is not in the agent list at all. Naming one
// remedy for every blocker therefore hands some users an instruction that
// cannot be carried out — the same class of defect this whole change set exists
// to remove, so the refusals classify their blockers instead.
//
// system_key, not kind, is the discriminator. Mika is deliberately kind='user'
// (it must stay visible and assignable) while still being product-owned and
// unarchivable; builder carriers are kind='system'.
type blockingAgentClass int

const (
	// blockingAgentUser is an ordinary workspace agent: rebind or archive it.
	blockingAgentUser blockingAgentClass = iota
	// blockingAgentMika is the workspace's built-in Mika. It cannot be archived
	// (agent.go rejects any system_key there) but it CAN be rebound: UpdateAgent
	// takes runtime_id for it like any other manageable agent. Those two halves
	// have to be stated separately or the remedy is wrong in one direction.
	blockingAgentMika
	// blockingAgentBuilderCarrier is the hidden carrier behind an unfinished
	// Agent Builder flow. It is released through that session, not the agent list.
	blockingAgentBuilderCarrier
	// blockingAgentOtherSystem is any future product-owned agent. Unarchivable
	// like the rest, with no remedy this code can name specifically.
	blockingAgentOtherSystem
)

// Class keys shared with the SQL CASE in ListActiveAgentsByProfile. That query
// has to classify server-side so its per-class counts can cover rows the LIMIT
// excludes, which means the mapping exists in two places; these constants give
// both the same vocabulary, and TestBlockingAgentClassMatchesSQLClassification
// pins them to each other.
const (
	blockingAgentClassKeyUser        = "user"
	blockingAgentClassKeyMika        = "mika"
	blockingAgentClassKeyBuilder     = "agent_builder"
	blockingAgentClassKeyOtherSystem = "other_system"
)

// Unknown keys fall to other_system, not user. Only the exact "user" key may
// produce the one class whose remedy claims an action ("reassign or archive"):
// if a future class is added to the SQL CASE and not here, the refusal should
// say it cannot name a remedy rather than confidently hand out one that does
// not apply.
func blockingAgentClassFromKey(key string) blockingAgentClass {
	switch key {
	case blockingAgentClassKeyUser:
		return blockingAgentUser
	case blockingAgentClassKeyMika:
		return blockingAgentMika
	case blockingAgentClassKeyBuilder:
		return blockingAgentBuilderCarrier
	default:
		return blockingAgentOtherSystem
	}
}

func classifyBlockingAgent(systemKey pgtype.Text) blockingAgentClass {
	key := strings.TrimSpace(systemKey.String)
	if !systemKey.Valid || key == "" {
		return blockingAgentUser
	}
	switch {
	case key == service.MikaSystemKey:
		return blockingAgentMika
	case strings.HasPrefix(key, agentBuilderSystemKeyPrefix):
		return blockingAgentBuilderCarrier
	default:
		return blockingAgentOtherSystem
	}
}

// blockingAgentLabel renders one blocker for a refusal sentence. Product-owned
// agents are marked so the reader can tell at a glance why the plain remedy
// does not apply to them.
func blockingAgentLabel(name, runtimeName, runtimeStatus string, class blockingAgentClass) string {
	switch class {
	case blockingAgentBuilderCarrier:
		return fmt.Sprintf("an unfinished Agent Builder session on %q (%s)", runtimeName, runtimeStatus)
	case blockingAgentMika, blockingAgentOtherSystem:
		return fmt.Sprintf("%q on %q (%s, built into Multica)", name, runtimeName, runtimeStatus)
	default:
		return fmt.Sprintf("%q on %q (%s)", name, runtimeName, runtimeStatus)
	}
}

// blockingAgentScope distinguishes the two refusals, because one remedy differs
// between them: moving Mika off this runtime clears an instance refusal, but a
// profile refusal only clears if the destination is not another runtime of the
// same profile.
type blockingAgentScope int

const (
	blockingAgentScopeInstance blockingAgentScope = iota
	blockingAgentScopeProfile
)

// blockingAgentRemedies returns one clause per distinct recovery path present,
// in a fixed order so the sentence is stable. An empty result means every
// blocker was product-owned and the caller should say so rather than suggest
// an action.
func blockingAgentRemedies(classes map[blockingAgentClass]bool, scope blockingAgentScope) []string {
	// "Reassign or archive them" must not appear to cover the product-owned
	// blockers listed beside them, which is exactly the instruction they cannot
	// follow. When both are present the clause names which ones it applies to.
	mixed := classes[blockingAgentMika] ||
		classes[blockingAgentBuilderCarrier] ||
		classes[blockingAgentOtherSystem]

	var out []string
	if classes[blockingAgentUser] && mixed {
		out = append(out, "The agents above that are not marked as built into Multica can be reassigned or archived.")
	} else if classes[blockingAgentUser] {
		out = append(out, "Reassign or archive them first.")
	}
	if classes[blockingAgentBuilderCarrier] {
		// Addressed to the creator, not to whoever hit this error. Builder
		// sessions are creator-scoped: ListAgentBuilderSessions returns 200 but
		// omits other members' sessions, and switch/discard go through
		// loadChatSessionForUser and return 403. So an admin who is not the
		// creator cannot even see the session to act on it, and telling them to
		// reopen it would be another instruction that cannot be carried out.
		out = append(out, "The unfinished Agent Builder session(s) here are hidden from the agent list, and only their creator can open them — ask the member who started the session to switch its runtime or discard it; another admin cannot do that for them.")
	}
	if classes[blockingAgentMika] {
		// Mika is unarchivable but NOT immovable: UpdateAgent takes runtime_id
		// for it like any other agent an admin can manage (the only system_key
		// guard in agent.go is on archive), and the agent detail page shows it
		// an editable runtime picker. An earlier version of this message said
		// there was no supported way to move it, which told an owner who could
		// have fixed this in one edit to give up instead — the exact failure
		// this whole change set is about. TestMikaRemedyMatchesWhatMikaCanDo
		// exercises both halves against the real endpoints so the claim cannot
		// drift from the product again.
		target := "another runtime"
		if scope == blockingAgentScopeProfile {
			target = "a runtime that this profile does not provide"
		}
		out = append(out, fmt.Sprintf(
			"Mika is built into Multica, so it cannot be archived — but it can be moved: open Mika's agent page and bind it to %s.",
			target,
		))
	}
	if classes[blockingAgentOtherSystem] {
		out = append(out, "Some blockers are agents built into Multica and cannot be archived.")
	}
	return out
}

// blockingAgentClassesFromAgents collects the classes present on a plain agent
// set, for the callers that already hold db.Agent rows.
func blockingAgentClassesFromAgents(agents []db.Agent) map[blockingAgentClass]bool {
	classes := make(map[blockingAgentClass]bool, 2)
	for _, a := range agents {
		classes[classifyBlockingAgent(a.SystemKey)] = true
	}
	return classes
}
