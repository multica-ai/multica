package handler

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A runtime or profile delete is refused while user agents are still bound.
// Telling the user to "reassign or archive them" is only true for agents they
// can actually reach: the archive endpoint rejects every system_key except
// Mika's. Naming one remedy for every blocker therefore hands some users an
// instruction that cannot be carried out — the same class of defect this whole
// change set exists to remove, so the refusals classify their blockers instead.
//
// Hidden kind='system' agents (the Agent Builder carriers) are never blockers:
// both delete guards list kind='user' only, and TeardownRuntime deletes the
// carriers with their runtime (GH #9762).
//
// system_key, not kind, is the discriminator. Mika is deliberately kind='user'
// (it must stay visible and assignable) while still being product-owned.
type blockingAgentClass int

const (
	// blockingAgentUser is an ordinary workspace agent: rebind or archive it.
	blockingAgentUser blockingAgentClass = iota
	// blockingAgentMika is the workspace's built-in Mika. It can be archived or
	// rebound like any manageable agent, but a profile refusal only clears if
	// the new runtime is outside that profile, so it gets its own clause.
	blockingAgentMika
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
	default:
		return blockingAgentOtherSystem
	}
}

func classifyBlockingAgent(systemKey pgtype.Text) blockingAgentClass {
	key := strings.TrimSpace(systemKey.String)
	if !systemKey.Valid || key == "" {
		return blockingAgentUser
	}
	if key == service.MikaSystemKey {
		return blockingAgentMika
	}
	return blockingAgentOtherSystem
}

// blockingAgentLabel renders one blocker for a refusal sentence. Product-owned
// agents are marked so the reader can tell at a glance why the plain remedy
// does not apply to them.
func blockingAgentLabel(name, runtimeName, runtimeStatus string, class blockingAgentClass) string {
	switch class {
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
	// "Reassign or archive them" must not appear to cover the unarchivable
	// blockers listed beside them, which is exactly the instruction they cannot
	// follow. When both are present the clause names which ones it applies to.
	mixed := classes[blockingAgentOtherSystem]

	var out []string
	if classes[blockingAgentUser] && mixed {
		out = append(out, "The agents above that are not marked as built into Multica can be reassigned or archived.")
	} else if classes[blockingAgentUser] {
		out = append(out, "Reassign or archive them first.")
	}
	if classes[blockingAgentMika] {
		// TestMikaRemedyMatchesWhatMikaCanDo exercises both remedies against
		// the real endpoints so the claim cannot drift from the product again.
		target := "another runtime"
		if scope == blockingAgentScopeProfile {
			target = "a runtime that this profile does not provide"
		}
		out = append(out, fmt.Sprintf(
			"Mika can be archived, or moved: open Mika's agent page and bind it to %s.",
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
