// Package agentpass — scope narrowing enforcement (JEH-1327 milestone 2).
//
// A child pass (issued for a sub-issue) may only narrow the scope that its
// parent pass allows. It may never widen it. This file owns:
//
//   - ScopeNarrows — pure predicate on two raw JSONB blobs.
//   - Service.ValidateChildScope — fetches the parent pass and delegates to
//     ScopeNarrows. Callers invoke this before creating a pass with a non-nil
//     parent_pass_id.
package agentpass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrScopeWiden is returned by ValidateChildScope when the proposed child
// scope would expand a permission that the parent scope restricts.
var ErrScopeWiden = errors.New("agentpass: child scope would widen parent scope")

// ScopeNarrows reports whether childScope is a valid narrowing of parentScope.
//
// Scope semantics (from the cerebro_agent_pass schema):
//
//	Empty/null/"{}" parent  → parent imposes no restrictions; any child is valid.
//	Empty/null/"{}" child   → child imposes no restrictions; it inherits parent.
//
// For each key present in childScope:
//   - If the key is absent from parentScope: child is adding a new restriction
//     (narrowing) — always valid.
//   - If both scopes carry the key as a JSON array: every element of the child
//     array must appear in the parent array (child ⊆ parent).
//   - If both scopes carry the key as a non-array value: the values must be
//     identical (no widening for scalar keys).
//
// Returns (false, ErrScopeWiden-wrapped error) when child widens parent.
// Returns (false, non-ErrScopeWiden error) on JSON parse failure.
func ScopeNarrows(parentScope, childScope []byte) (bool, error) {
	if scopeIsEmpty(parentScope) {
		return true, nil
	}
	if scopeIsEmpty(childScope) {
		return true, nil
	}

	var parent map[string]json.RawMessage
	if err := json.Unmarshal(parentScope, &parent); err != nil {
		return false, fmt.Errorf("parse parent scope: %w", err)
	}
	var child map[string]json.RawMessage
	if err := json.Unmarshal(childScope, &child); err != nil {
		return false, fmt.Errorf("parse child scope: %w", err)
	}

	for key, childVal := range child {
		parentVal, parentHasKey := parent[key]
		if !parentHasKey {
			continue
		}
		if !scopeValNarrows(parentVal, childVal) {
			return false, fmt.Errorf("%w: key %q child %s not a subset of parent %s",
				ErrScopeWiden, key, childVal, parentVal)
		}
	}
	return true, nil
}

// scopeIsEmpty returns true for nil, zero-length, "null", or "{}".
func scopeIsEmpty(s []byte) bool {
	if len(s) == 0 {
		return true
	}
	str := string(s)
	return str == "null" || str == "{}"
}

// scopeValNarrows reports whether childVal is a valid narrowing of parentVal.
// Arrays: child must be a subset of parent. Scalars: must be identical.
func scopeValNarrows(parentVal, childVal json.RawMessage) bool {
	var parentArr, childArr []json.RawMessage
	if json.Unmarshal(parentVal, &parentArr) == nil && json.Unmarshal(childVal, &childArr) == nil {
		parentSet := make(map[string]struct{}, len(parentArr))
		for _, v := range parentArr {
			parentSet[string(v)] = struct{}{}
		}
		for _, v := range childArr {
			if _, ok := parentSet[string(v)]; !ok {
				return false
			}
		}
		return true
	}
	return string(parentVal) == string(childVal)
}

// ValidateChildScope checks that newScope is a valid narrowing of the scope
// on the pass identified by parentPassID.
//
// Returns nil when:
//   - parentPassID is not valid (root pass, no parent to narrow against).
//   - The parent pass is not found (treat as no restriction, allow).
//   - newScope is a valid narrowing of the parent's scope.
//
// Returns ErrScopeWiden (detectable via errors.Is) when the child would
// expand a parent restriction. Callers should reject the pass creation with
// HTTP 422 or equivalent.
func (s *Service) ValidateChildScope(ctx context.Context, parentPassID pgtype.UUID, newScope []byte) error {
	if s == nil || s.queries == nil {
		return ErrNilQueries
	}
	if !parentPassID.Valid {
		return nil
	}

	parent, err := s.queries.GetCerebroAgentPass(ctx, parentPassID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("fetch parent pass: %w", err)
	}

	ok, err := ScopeNarrows(parent.Scope, newScope)
	if err != nil {
		return err
	}
	if !ok {
		return ErrScopeWiden
	}
	return nil
}
