package handler

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// FIR-4930 — the precedence rule in the list filter is the part that is easy to
// get wrong: OR-ing the explicit stamp next to the derived agent_task chain
// would still match the autopilot creator on an issue that was deliberately
// stamped for someone else, so the wrong human would keep seeing the work.
// The derived branch must be gated on the explicit column being NULL.
func TestOnBehalfOfWherePredicate_ExplicitStampSuppressesDerivedChain(t *testing.T) {
	args := 0
	addArg := func(any) string {
		args++
		return "$1"
	}

	sql := onBehalfOfWherePredicate([]pgtype.UUID{{}}, addArg)

	if !strings.Contains(sql, "i.on_behalf_of_user_id = ANY($1::uuid[])") {
		t.Errorf("predicate must match the explicit stamp directly:\n%s", sql)
	}

	derived := strings.Index(sql, "origin_type = 'agent_task'")
	if derived < 0 {
		t.Fatalf("predicate lost the derived agent_task branch:\n%s", sql)
	}
	guard := strings.Index(sql, "i.on_behalf_of_user_id IS NULL")
	if guard < 0 || guard > derived {
		t.Errorf("derived branch must be gated behind an IS NULL guard on the explicit stamp:\n%s", sql)
	}

	if args != 1 {
		t.Errorf("expected the id list to be bound once, got %d bindings", args)
	}
}

// The predicate is dropped into a larger WHERE clause built by the list and
// count handlers, so it has to be self-parenthesised — otherwise its top-level
// OR would swallow the sibling filters (status, project, assignee) and the list
// would return issues the caller did not ask for.
func TestOnBehalfOfWherePredicate_IsSelfContained(t *testing.T) {
	sql := strings.TrimSpace(onBehalfOfWherePredicate(nil, func(any) string { return "$1" }))

	if !strings.HasPrefix(sql, "(") || !strings.HasSuffix(sql, ")") {
		t.Errorf("predicate must be wrapped in its own parentheses:\n%s", sql)
	}
}
