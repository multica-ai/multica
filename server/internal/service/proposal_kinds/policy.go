package proposalkinds

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

// Standing approvals (spec 4.4). A policy pre-authorises a narrow class of
// proposals. The vocabulary lives here so the kind that creates a policy and the
// engine that applies one read the same rules.

// Kind keys the standing approval rules name. Only KeyOpportunity and KeyDecision
// are registered today; the others are named so that a later kind of the same
// name is refused as a policy target from the day it exists.
const KeyStandingApproval = "standing_approval"

// policyKindBudget is the budget proposal kind's key. It is named here, not
// taken from the budget kind, so the forbidden list holds whatever the budget
// work names its constant.
const policyKindBudget = "budget"

// Refusal reasons from the policy rules. They name why a proposal was not
// auto-approved, for the governance entry and tests. The strings are stable.
const (
	PolicyRefusalNoPolicy      = "no_policy"
	PolicyRefusalPaused        = "company_paused"
	PolicyRefusalExpired       = "expired"
	PolicyRefusalKind          = "kind"
	PolicyRefusalForbidden     = "forbidden_class"
	PolicyRefusalUrgent        = "urgent"
	PolicyRefusalAutonomy      = "autonomy_level"
	PolicyRefusalSquad         = "squad"
	PolicyRefusalFloor         = "floor"
	PolicyRefusalPayload       = "payload"
	PolicyRefusalStage         = "stage"
	PolicyRefusalCostUnknown   = "cost_unknown"
	PolicyRefusalCost          = "cost"
	PolicyRefusalBudgetUnknown = "budget_unknown"
	PolicyRefusalBudget        = "budget"
	PolicyRefusalTags          = "tags"
	PolicyRefusalCap           = "weekly_cap"
)

// PolicyDefaultMaxPerWeek is the weekly cap a policy gets when it names none.
const PolicyDefaultMaxPerWeek = 3

// PolicyNameMax bounds a policy name, as the table's CHECK does.
const PolicyNameMax = 100

// ForbiddenForStandingApproval reports why no standing approval may decide a
// proposal of this kind, or whether it is urgent (spec 4.4). The list is code, not
// configuration: a policy cannot be written to allow these. It returns "" when
// the proposal may be auto-approved on the rules of its policy.
//
//   - org_change: organisation changes, including autonomy changes, are the boss's.
//   - standing_approval: a policy is the boss's to create or change.
//   - stage_gate: a gate that leaves the launch stage. Launch is the boss's call
//     (spec 3.6 and 4.4). Other gates may be approved by a policy.
//   - urgent: an urgent proposal asks for the boss's attention now.
//
// A budget proposal is not on the list: it is decided by the remaining-budget
// rule, because a raise above the squad's remaining budget is the spend the
// policy must not cover.
//
// payload is the filing's payload, which the stage gate rule reads. A nil payload
// means the kind alone is being checked, as when a policy is created; a stage gate
// policy is then allowed, and each filing is checked against its own stage.
func ForbiddenForStandingApproval(kind string, urgent bool, payload []byte) string {
	if urgent {
		return PolicyRefusalUrgent
	}
	switch kind {
	case KeyOrgChange, KeyStandingApproval:
		return PolicyRefusalForbidden
	case KeyStageGate:
		if payload == nil {
			return ""
		}
		var g Gate
		// A payload that does not decode is refused: the stage cannot be shown
		// to be something other than launch.
		if err := json.Unmarshal(payload, &g); err != nil || g.From == StageKeyLaunch {
			return PolicyRefusalForbidden
		}
	}
	return ""
}

// StageKeyLaunch is the key of the Launch stage in the default pipeline. Its
// gate is the go-live decision that no standing approval may make.
const StageKeyLaunch = "launch"

// PolicyMatch is the match JSON of a policy (spec 4.4). An empty field does not
// narrow the match. Tags must all be on the proposal (see ProposalTags). A
// policy naming one could never be checked.
type PolicyMatch struct {
	SquadIDs      []string       `json:"squad_ids,omitempty"`
	FloorIDs      []string       `json:"floor_ids,omitempty"`
	MaxCostMicros *int64         `json:"max_cost_micros,omitempty"`
	PayloadEquals map[string]any `json:"payload_equals,omitempty"`
	Tags          []string       `json:"tags,omitempty"`
}

// PolicySpec is what creates a policy: from the boss's API call or from an
// approved standing_approval proposal, whose payload is this shape.
type PolicySpec struct {
	Name  string      `json:"name"`
	Kind  string      `json:"kind"`
	Match PolicyMatch `json:"match"`
	// MaxPerWeek is the weekly cap. Nil means PolicyDefaultMaxPerWeek.
	MaxPerWeek *int32 `json:"max_per_week,omitempty"`
	// ValidUntil is the last day the policy applies, YYYY-MM-DD. Empty means no end.
	ValidUntil string `json:"valid_until,omitempty"`
}

// KindLookup reports whether a kind key is a registered proposal kind.
type KindLookup func(key string) bool

// ValidatePolicySpec checks a policy and returns it in canonical form, with the
// defaults filled in. known says which kinds exist. now is the clock for the end
// date, which may not be in the past.
func ValidatePolicySpec(spec PolicySpec, known KindLookup, now time.Time) (PolicySpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if n := len([]rune(spec.Name)); n < 1 || n > PolicyNameMax {
		return PolicySpec{}, fmt.Errorf("name must be 1 to %d characters", PolicyNameMax)
	}
	spec.Kind = strings.TrimSpace(spec.Kind)
	if reason := ForbiddenForStandingApproval(spec.Kind, false, nil); reason != "" {
		return PolicySpec{}, fmt.Errorf("kind %q can never be approved by a standing approval", spec.Kind)
	}
	if !known(spec.Kind) {
		return PolicySpec{}, fmt.Errorf("kind %q is not a proposal kind", spec.Kind)
	}
	if spec.MaxPerWeek == nil {
		def := int32(PolicyDefaultMaxPerWeek)
		spec.MaxPerWeek = &def
	}
	if *spec.MaxPerWeek < 1 || *spec.MaxPerWeek > 50 {
		return PolicySpec{}, errors.New("max_per_week must be between 1 and 50")
	}
	for _, id := range spec.Match.SquadIDs {
		if !validUUID(id) {
			return PolicySpec{}, fmt.Errorf("match.squad_ids: %q is not a squad id", id)
		}
	}
	for _, id := range spec.Match.FloorIDs {
		if !validUUID(id) {
			return PolicySpec{}, fmt.Errorf("match.floor_ids: %q is not a floor id", id)
		}
	}
	if spec.Match.MaxCostMicros != nil && *spec.Match.MaxCostMicros < 0 {
		return PolicySpec{}, errors.New("match.max_cost_micros must not be negative")
	}
	tags, err := normalizeTags(spec.Match.Tags)
	if err != nil {
		return PolicySpec{}, fmt.Errorf("match.tags: %w", err)
	}
	spec.Match.Tags = tags
	for key := range spec.Match.PayloadEquals {
		if strings.TrimSpace(key) == "" {
			return PolicySpec{}, errors.New("match.payload_equals keys must not be empty")
		}
	}
	if spec.ValidUntil != "" {
		day, err := time.Parse("2006-01-02", spec.ValidUntil)
		if err != nil {
			return PolicySpec{}, errors.New("valid_until must be a date as YYYY-MM-DD")
		}
		if day.Before(now.UTC().Truncate(24 * time.Hour)) {
			return PolicySpec{}, errors.New("valid_until must not be in the past")
		}
	}
	return spec, nil
}

// EncodePolicySpec returns the canonical JSON of a validated spec. It is what a
// standing_approval proposal stores as its payload.
func EncodePolicySpec(spec PolicySpec) ([]byte, error) {
	return json.Marshal(spec)
}

// DecodePolicySpec reads a spec from a payload, refusing unknown fields so a
// misspelt field fails rather than being dropped.
func DecodePolicySpec(payload []byte) (PolicySpec, error) {
	var spec PolicySpec
	if err := decodeObject(payload, &spec); err != nil {
		return PolicySpec{}, err
	}
	return spec, nil
}

func validUUID(s string) bool {
	_, err := util.ParseUUID(s)
	return err == nil
}

// Tag limits for proposals and policy matches. A tag is a lowercase label, such as
// "docs" or "q4-launch".
const (
	proposalTagsMax   = 10
	proposalTagLenMax = 40
)

// ProposalTags reads a proposal's tags from its payload's `tags` array. Entries
// that are not strings, or are empty after trimming, are dropped; the rest are
// lowercased, de-duplicated and capped. A payload with no tags gives an empty list
// (never nil), which is what the proposal column stores.
func ProposalTags(payload []byte) []string {
	var obj struct {
		Tags []any `json:"tags"`
	}
	out := []string{}
	if err := json.Unmarshal(payload, &obj); err != nil {
		return out
	}
	var raw []string
	for _, t := range obj.Tags {
		if str, ok := t.(string); ok {
			raw = append(raw, str)
		}
	}
	tags, err := normalizeTags(raw)
	if err != nil {
		// Over-long or over-many tags are cut rather than failing the filing: the
		// tags only narrow a standing approval, never widen it.
		return cleanTags(raw)
	}
	return append(out, tags...)
}

// cleanTags is the lenient form of normalizeTags, used for filing.
func cleanTags(raw []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] || len([]rune(t)) > proposalTagLenMax {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == proposalTagsMax {
			break
		}
	}
	return out
}

// normalizeTags is the strict form for a policy's match: a tag that is empty or
// too long, or more than the limit, is refused.
func normalizeTags(raw []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			return nil, errors.New("tags must not be empty")
		}
		if len([]rune(t)) > proposalTagLenMax {
			return nil, fmt.Errorf("tag %q is longer than %d characters", t, proposalTagLenMax)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > proposalTagsMax {
		return nil, fmt.Errorf("at most %d tags", proposalTagsMax)
	}
	return out, nil
}
