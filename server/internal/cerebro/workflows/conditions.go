package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Condition is a single conjunctive filter applied after the trigger fires.
// The conditions column in cerebro_workflow stores an array of these — all
// must evaluate true for the action to execute. An empty array means
// "always".
//
//	{ "field": "issue.priority", "op": "in", "values": ["urgent","high"] }
//	{ "field": "issue.project_id", "op": "eq", "value": "<uuid>" }
//	{ "field": "issue.assignee_id", "op": "is_not_null" }
type Condition struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value,omitempty"`
	Values []any  `json:"values,omitempty"`
}

func parseConditions(raw []byte) ([]Condition, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out []Condition
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("conditions: %w", err)
	}
	return out, nil
}

// evaluate returns true when every condition matches the given context.
// Unknown fields and unknown operators fail closed (return false) — better
// to skip a workflow than fire one with an unexpected interpretation.
//
// Note: this only handles the pure (no-DB-access) ops. Ops that need a DB
// lookup (currently only `evidence_present`) are routed through
// Service.conditionsHold before this is called, and short-circuit there.
func evaluate(conds []Condition, ctx map[string]any) bool {
	for _, c := range conds {
		if !match(c, ctx) {
			return false
		}
	}
	return true
}

func match(c Condition, ctx map[string]any) bool {
	got, ok := lookup(c.Field, ctx)
	switch c.Op {
	case "eq":
		return ok && fmt.Sprint(got) == fmt.Sprint(c.Value)
	case "ne":
		return ok && fmt.Sprint(got) != fmt.Sprint(c.Value)
	case "in":
		if !ok {
			return false
		}
		for _, v := range c.Values {
			if fmt.Sprint(got) == fmt.Sprint(v) {
				return true
			}
		}
		return false
	case "is_null":
		return !ok || got == nil
	case "is_not_null":
		return ok && got != nil
	default:
		return false
	}
}

// parseEvidenceConfig extracts EvidenceConfig from a Condition.Value field.
// Accepts either the typed struct (when callers build conditions in Go) or
// a generic map[string]any (the JSON-unmarshalled shape). Returns the
// zero-value EvidenceConfig + nil error when Value is nil so the engine can
// apply defaults.
func parseEvidenceConfig(v any) (EvidenceConfig, error) {
	if v == nil {
		return EvidenceConfig{}, nil
	}
	if cfg, ok := v.(EvidenceConfig); ok {
		return cfg, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return EvidenceConfig{}, fmt.Errorf("evidence: marshal value: %w", err)
	}
	var cfg EvidenceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return EvidenceConfig{}, fmt.Errorf("evidence: %w", err)
	}
	return cfg, nil
}

// prURLPattern recognises the most common PR/MR shapes: GitHub
// `/<org>/<repo>/pull/<n>` and GitLab `/<org>/<repo>/-/merge_requests/<n>`.
// Both are case-insensitive and require a numeric ID at the tail so a stray
// "/pull/" in some other URL doesn't false-match.
var prURLPattern = regexp.MustCompile(`(?i)https?://[^\s)>"']*(?:/pull/|/-/merge_requests/)\d+`)

const (
	defaultEvidenceLookback = 20
	maxEvidenceLookback     = 200
)

// evaluateEvidence answers whether the triggered issue currently carries
// evidence of work — a PR URL in any of the recent comments / description,
// an image attachment anywhere on the issue or its comments, or a URL that
// matches the configured regex. The check is "any-of": if Require lists
// three signals, any single match is enough.
//
// Empty Require defaults to []string{"pr_url"}, the most common case.
// LookbackComments defaults to 20 and is capped at 200 to bound the scan.
//
// Errors propagate up to conditionsHold (which fails closed on them).
// "No evidence" is NOT an error — it returns (false, nil) so the workflow
// is simply skipped, exactly the same way an `eq` condition not matching
// would skip it.
func (s *Service) evaluateEvidence(ctx context.Context, wf workflow, te TriggerEvent, cond Condition) (bool, error) {
	if te.IssueID == "" {
		return false, errors.New("evidence_present: trigger event has no issue_id")
	}
	cfg, err := parseEvidenceConfig(cond.Value)
	if err != nil {
		return false, err
	}
	require := cfg.Require
	if len(require) == 0 {
		require = []string{EvidenceKindPRURL}
	}
	lookback := cfg.LookbackComments
	if lookback <= 0 {
		lookback = defaultEvidenceLookback
	}
	if lookback > maxEvidenceLookback {
		lookback = maxEvidenceLookback
	}

	wantPR := false
	wantImage := false
	wantRegex := false
	for _, r := range require {
		switch r {
		case EvidenceKindPRURL:
			wantPR = true
		case EvidenceKindImageAttachment:
			wantImage = true
		case EvidenceKindURLRegex:
			wantRegex = true
		}
		// Unknown values are deliberately ignored — see EvidenceConfig
		// docs for the forward-compat rationale.
	}
	if !wantPR && !wantImage && !wantRegex {
		// All require entries were unknown — treat as no usable signal.
		return false, nil
	}

	var userRegex *regexp.Regexp
	if wantRegex {
		if cfg.URLRegex == "" {
			return false, errors.New("evidence_present: url_regex required when 'url_regex' is in require")
		}
		userRegex, err = regexp.Compile(cfg.URLRegex)
		if err != nil {
			return false, fmt.Errorf("evidence_present: compile url_regex: %w", err)
		}
	}

	issueID, err := parseUUID(te.IssueID)
	if err != nil {
		return false, fmt.Errorf("evidence_present: %w", err)
	}
	wsID, err := parseUUID(te.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("evidence_present: %w", err)
	}

	if wantPR || wantRegex {
		// Description is in te.Raw[issue][description] when the listener
		// promoted it; if not present, that's fine — comments cover the
		// same ground.
		if issueMap, ok := te.Raw["issue"].(map[string]any); ok {
			desc, _ := issueMap["description"].(string)
			if desc != "" {
				if wantPR && prURLPattern.MatchString(desc) {
					return true, nil
				}
				if wantRegex && userRegex.MatchString(desc) {
					return true, nil
				}
			}
		}
		comments, err := s.issues.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
			IssueID:     issueID,
			WorkspaceID: wsID,
			Limit:       int32(lookback),
		})
		if err != nil {
			return false, fmt.Errorf("evidence_present: list comments: %w", err)
		}
		// Scan newest-first so the typical "agent posted PR link as last
		// comment" path bails early. ListCommentsForIssue returns ASC, so
		// iterate the slice in reverse.
		for i := len(comments) - 1; i >= 0; i-- {
			content := comments[i].Content
			if wantPR && prURLPattern.MatchString(content) {
				return true, nil
			}
			if wantRegex && userRegex.MatchString(content) {
				return true, nil
			}
		}
	}

	if wantImage {
		atts, err := s.issues.ListAttachmentsByIssue(ctx, db.ListAttachmentsByIssueParams{
			IssueID:     issueID,
			WorkspaceID: wsID,
		})
		if err != nil {
			return false, fmt.Errorf("evidence_present: list issue attachments: %w", err)
		}
		for _, a := range atts {
			if isImageContentType(a.ContentType) {
				return true, nil
			}
		}
		// Comment attachments aren't covered by ListAttachmentsByIssue.
		// Fall back to URL list — caller picks them up via filename
		// suffix when the attachment is bound to a comment.
		urls, err := s.issues.ListAttachmentURLsByIssueOrComments(ctx, issueID)
		if err != nil {
			return false, fmt.Errorf("evidence_present: list attachment urls: %w", err)
		}
		for _, u := range urls {
			if hasImageURLSuffix(u) {
				return true, nil
			}
		}
	}

	return false, nil
}

func isImageContentType(ct string) bool {
	return strings.HasPrefix(strings.ToLower(ct), "image/")
}

// hasImageURLSuffix is a cheap substring check used as a fallback when we
// only have an attachment URL and no content-type. Suffixes match the most
// common image MIME types we accept.
func hasImageURLSuffix(u string) bool {
	lower := strings.ToLower(u)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".heic", ".bmp"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// lookup walks dotted field paths like "issue.priority" against the supplied
// context map. Missing keys return (nil, false) so callers can distinguish
// "field absent" from "field is JSON null".
func lookup(path string, ctx map[string]any) (any, bool) {
	if path == "" {
		return nil, false
	}
	cur := any(ctx)
	for _, seg := range splitDot(path) {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, exists := m[seg]
		if !exists {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func splitDot(s string) []string {
	out := make([]string, 0, 2)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
