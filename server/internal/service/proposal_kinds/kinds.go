// Package proposalkinds holds the handlers for proposal kinds (spec 3.2).
//
// A kind says what a filing carries (Validate), what a summary of it reads
// (Summarize), and what its decision does (Apply on approval, Reject on
// rejection). The service package dispatches to these handlers. This package
// imports no service code: the kinds that act on initiatives reach them through
// the Initiatives interface, which the service implements.
package proposalkinds

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Kind keys, as stored in proposal.kind and accepted by the API.
const (
	KeyDecision    = "decision"
	KeyOpportunity = "opportunity"
	KeyStageGate   = "stage_gate"
)

// ErrUnknownKind refuses a filing or lookup with a kind no handler is registered
// for. The API answers it as a bad request.
var ErrUnknownKind = errors.New("unknown proposal kind")

// ErrSquadNotLive: a kind's squad (a handoff squad) is missing or archived.
var ErrSquadNotLive = errors.New("squad not found in this workspace")

// Env is what Validate may read. Q is the caller's queries, outside any
// decision transaction.
type Env struct {
	Q           *db.Queries
	WorkspaceID pgtype.UUID
	// SquadID is the squad filing the proposal, which a kind may limit.
	SquadID pgtype.UUID
}

// Decision is an approval or rejection, with the rows it settles. Proposal and
// Issue are as they were before the decision; the caller has locked Proposal.
type Decision struct {
	// Tx is the decision transaction; q is bound to it. Kinds that need a
	// raw query (the issue position helper) use Tx.
	Tx          pgx.Tx
	WorkspaceID pgtype.UUID
	Proposal    db.Proposal
	Issue       db.Issue
	DeciderID   pgtype.UUID
	Note        string
	// Outcome is the boss's choice on an operate-stage gate: continue, scale or
	// sunset. Empty on every other decision.
	Outcome string
}

// Effect reports what Apply created that the caller must publish or kick off
// after the commit.
type Effect struct {
	// Project is the handoff project of a decision approved with a handoff squad.
	Project *db.Project
}

// Kind is one proposal kind.
type Kind interface {
	Key() string
	// FiledByAgents reports whether a squad may file this kind through the API.
	// The system files the rest (stage gates).
	FiledByAgents() bool
	// Validate checks the payload at filing and returns the canonical form to store.
	Validate(ctx context.Context, env Env, payload []byte) ([]byte, error)
	// Summarize is a one-line reading of the payload for lists and the brief.
	Summarize(payload []byte) string
	// Apply runs inside the approval transaction.
	Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error)
	// Reject runs inside the rejection transaction.
	Reject(ctx context.Context, q *db.Queries, d Decision) error
}

// Initiatives is what the opportunity and stage_gate kinds need from the
// initiative service. Each method runs in the caller's transaction.
type Initiatives interface {
	// OpenFromOpportunity creates the initiative an approved opportunity asks for.
	OpenFromOpportunity(ctx context.Context, q *db.Queries, d Decision, o Opportunity) error
	// AdvanceFromGate moves an initiative on an approved gate.
	AdvanceFromGate(ctx context.Context, q *db.Queries, d Decision, g Gate) error
	// ReturnFromGate leaves an initiative at its stage on a rejected gate and
	// wakes the leader with the boss's note.
	ReturnFromGate(ctx context.Context, q *db.Queries, d Decision, g Gate) error
}

// Registry maps kind keys to handlers. A new kind is one file and one entry in
// NewRegistry.
type Registry struct {
	kinds map[string]Kind
}

// NewRegistry registers every kind. initiatives serves the kinds that act on
// initiatives.
func NewRegistry(initiatives Initiatives) *Registry {
	list := []Kind{
		decisionKind{},
		opportunityKind{initiatives: initiatives},
		stageGateKind{initiatives: initiatives},
	}
	r := &Registry{kinds: make(map[string]Kind, len(list))}
	for _, k := range list {
		r.kinds[k.Key()] = k
	}
	return r
}

// Lookup returns the handler for a kind key. The empty key is a decision, as
// it was before kinds existed.
func (r *Registry) Lookup(key string) (Kind, error) {
	if key == "" {
		key = KeyDecision
	}
	k, ok := r.kinds[key]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, key)
	}
	return k, nil
}

// decodeObject parses a payload that must be a JSON object, with unknown fields
// refused so a misspelt field fails at filing rather than being dropped.
func decodeObject(payload []byte, into any) error {
	if len(payload) == 0 || string(payload) == "null" {
		payload = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	return nil
}
