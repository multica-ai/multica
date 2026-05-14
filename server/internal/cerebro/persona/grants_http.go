package persona

// CEREBRO-PATCH(persona-grants-http-backend): JEH-1181 HTTP backend that
// proxies persona.Backend calls to /api/workspaces/{id}/grants. Replaces the
// in-memory MockBackend now that JEH-1179 (grants API) is on main.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// apiGrant is the wire shape returned by /api/workspaces/{id}/grants.
// Kept local so we don't import the handler package.
type apiGrant struct {
	ID                    string  `json:"id"`
	WorkspaceID           string  `json:"workspace_id"`
	SubjectType           string  `json:"subject_type"`
	SubjectID             *string `json:"subject_id"`
	ResourcePattern       string  `json:"resource_pattern"`
	Capability            string  `json:"capability"`
	ClassificationCeiling *string `json:"classification_ceiling"`
	TimeWindowStart       *string `json:"time_window_start"`
	TimeWindowEnd         *string `json:"time_window_end"`
	ApprovalRequired      bool    `json:"approval_required"`
	Status                string  `json:"status"`
	GrantedByID           *string `json:"granted_by_id"`
	GrantedAt             string  `json:"granted_at"`
	UpdatedAt             string  `json:"updated_at"`
}

func apiGrantToPersona(g apiGrant) Grant {
	var startsAt, endsAt *time.Time
	if g.TimeWindowStart != nil && *g.TimeWindowStart != "" {
		if t, err := time.Parse(time.RFC3339, *g.TimeWindowStart); err == nil {
			startsAt = &t
		}
	}
	if g.TimeWindowEnd != nil && *g.TimeWindowEnd != "" {
		if t, err := time.Parse(time.RFC3339, *g.TimeWindowEnd); err == nil {
			endsAt = &t
		}
	}
	createdAt, _ := time.Parse(time.RFC3339, g.GrantedAt)
	updatedAt, _ := time.Parse(time.RFC3339, g.UpdatedAt)
	subjectID := ""
	if g.SubjectID != nil {
		subjectID = *g.SubjectID
	}
	classificationCeiling := ""
	if g.ClassificationCeiling != nil {
		classificationCeiling = *g.ClassificationCeiling
	}
	createdBy := ""
	if g.GrantedByID != nil {
		createdBy = *g.GrantedByID
	}
	return Grant{
		ID:                    g.ID,
		WorkspaceID:           g.WorkspaceID,
		SubjectType:           g.SubjectType,
		SubjectID:             subjectID,
		ResourcePattern:       g.ResourcePattern,
		Capability:            g.Capability,
		ClassificationCeiling: classificationCeiling,
		StartsAt:              startsAt,
		EndsAt:                endsAt,
		RequiresApproval:      g.ApprovalRequired,
		Effect:                EffectAllow, // API only tracks status, not allow/deny
		Status:                g.Status,
		CreatedAt:             createdAt,
		UpdatedAt:             updatedAt,
		CreatedBy:             createdBy,
	}
}

// HTTPBackend proxies Backend calls to /api/workspaces/{id}/grants.
type HTTPBackend struct {
	client      *cli.APIClient
	workspaceID string
}

// NewHTTPBackend returns a Backend backed by the Multica grants HTTP API.
func NewHTTPBackend(client *cli.APIClient, workspaceID string) *HTTPBackend {
	return &HTTPBackend{client: client, workspaceID: workspaceID}
}

func (h *HTTPBackend) basePath() string {
	return "/api/workspaces/" + url.PathEscape(h.workspaceID) + "/grants"
}

func (h *HTTPBackend) List(ctx context.Context, f ListFilter, _ AuditContext) ([]Grant, error) {
	q := url.Values{}
	if f.SubjectType != "" {
		q.Set("subject_type", f.SubjectType)
	}
	if f.SubjectID != "" {
		q.Set("subject_id", f.SubjectID)
	}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	path := h.basePath()
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp struct {
		Grants []apiGrant `json:"grants"`
	}
	if err := h.client.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	out := make([]Grant, 0, len(resp.Grants))
	for _, g := range resp.Grants {
		pg := apiGrantToPersona(g)
		// Apply filters the API doesn't support server-side.
		if f.Capability != "" && pg.Capability != f.Capability {
			continue
		}
		if f.ResourceLike != "" {
			if !strings.Contains(pg.ResourcePattern, f.ResourceLike) {
				continue
			}
		}
		if f.Effect != "" && pg.Effect != f.Effect {
			continue
		}
		out = append(out, pg)
	}
	// Stable ordering (API returns newest first).
	sortGrantsNewestFirst(out)
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			return nil, nil
		}
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (h *HTTPBackend) Get(ctx context.Context, id string, _ AuditContext) (*Grant, error) {
	var g apiGrant
	if err := h.client.GetJSON(ctx, h.basePath()+"/"+url.PathEscape(id), &g); err != nil {
		return nil, fmt.Errorf("get grant %s: %w", id, err)
	}
	pg := apiGrantToPersona(g)
	return &pg, nil
}

func (h *HTTPBackend) Create(ctx context.Context, in GrantInput, _ AuditContext) (*Grant, error) {
	body := map[string]any{}
	if in.SubjectType != nil {
		body["subject_type"] = *in.SubjectType
	}
	if in.SubjectID != nil {
		body["subject_id"] = *in.SubjectID
	}
	if in.ResourcePattern != nil {
		body["resource_pattern"] = *in.ResourcePattern
	}
	if in.Capability != nil {
		body["capability"] = *in.Capability
	}
	if in.ClassificationCeiling != nil {
		body["classification_ceiling"] = *in.ClassificationCeiling
	}
	if in.StartsAt != nil {
		body["time_window_start"] = in.StartsAt.Format(time.RFC3339)
	}
	if in.EndsAt != nil {
		body["time_window_end"] = in.EndsAt.Format(time.RFC3339)
	}
	if in.RequiresApproval != nil {
		body["approval_required"] = *in.RequiresApproval
	}
	var g apiGrant
	if err := h.client.PostJSON(ctx, h.basePath(), body, &g); err != nil {
		return nil, fmt.Errorf("create grant: %w", err)
	}
	pg := apiGrantToPersona(g)
	return &pg, nil
}

func (h *HTTPBackend) Update(ctx context.Context, id string, in GrantInput, _ AuditContext) (*Grant, error) {
	body := map[string]any{}
	if in.ResourcePattern != nil {
		body["resource_pattern"] = *in.ResourcePattern
	}
	if in.Capability != nil {
		body["capability"] = *in.Capability
	}
	if in.ClassificationCeiling != nil {
		body["classification_ceiling"] = *in.ClassificationCeiling
	}
	if in.StartsAt != nil {
		body["time_window_start"] = in.StartsAt.Format(time.RFC3339)
	}
	if in.EndsAt != nil {
		body["time_window_end"] = in.EndsAt.Format(time.RFC3339)
	}
	if in.RequiresApproval != nil {
		body["approval_required"] = *in.RequiresApproval
	}
	if in.Status != nil {
		body["status"] = *in.Status
	}
	var g apiGrant
	if err := h.client.PatchJSON(ctx, h.basePath()+"/"+url.PathEscape(id), body, &g); err != nil {
		return nil, fmt.Errorf("update grant %s: %w", id, err)
	}
	pg := apiGrantToPersona(g)
	return &pg, nil
}

func (h *HTTPBackend) Delete(ctx context.Context, id string, _ AuditContext) error {
	if err := h.client.DeleteJSON(ctx, h.basePath()+"/"+url.PathEscape(id)); err != nil {
		return fmt.Errorf("delete grant %s: %w", id, err)
	}
	return nil
}

// Evaluate implements policy evaluation client-side by fetching active grants
// for the actor and checking resource/capability/classification match.
// A deny-override pattern (first explicit deny wins) applies when multiple
// grants match.
func (h *HTTPBackend) Evaluate(ctx context.Context, q EvaluationQuery, ac AuditContext) (*EvaluationResult, error) {
	now := time.Now().UTC()
	if q.At != nil {
		now = *q.At
	}

	// Fetch active grants for the subject (actor) and workspace_default.
	actorGrants, err := h.List(ctx, ListFilter{
		SubjectType: q.ActorType,
		SubjectID:   q.ActorID,
		Status:      StatusActive,
	}, ac)
	if err != nil {
		return nil, fmt.Errorf("evaluate: fetch actor grants: %w", err)
	}
	wsGrants, err := h.List(ctx, ListFilter{
		SubjectType: SubjectWorkspaceDefault,
		Status:      StatusActive,
	}, ac)
	if err != nil {
		return nil, fmt.Errorf("evaluate: fetch workspace grants: %w", err)
	}
	all := append(actorGrants, wsGrants...)

	var matched []Grant
	for _, g := range all {
		if !matchesGrant(g, q, now) {
			continue
		}
		matched = append(matched, g)
	}

	result := &EvaluationResult{
		Decision:      "deny",
		Reason:        "no matching active grant",
		MatchedGrants: matched,
		EvaluatedAt:   time.Now().UTC(),
		Query:         q,
	}
	if len(matched) == 0 {
		return result, nil
	}

	// Deny wins over allow.
	for i, g := range matched {
		if g.Effect == EffectDeny {
			result.Decision = "deny"
			result.Reason = "explicit deny grant matched"
			result.WinningGrant = &matched[i]
			return result, nil
		}
	}
	result.Decision = "allow"
	result.Reason = "allow grant matched"
	result.WinningGrant = &matched[0]
	return result, nil
}

// ListAudit is not yet supported via the HTTP API (no audit endpoint in JEH-1179).
// Returns empty until a grant audit endpoint is added.
func (h *HTTPBackend) ListAudit(_ context.Context, _ string, _ int) ([]AuditEntry, error) {
	return nil, nil
}

// matchesGrant checks resource pattern, capability, classification, and time window.
func matchesGrant(g Grant, q EvaluationQuery, now time.Time) bool {
	if !globMatch(g.ResourcePattern, q.Resource) {
		return false
	}
	if g.Capability != q.Capability && !globMatch(g.Capability, q.Capability) {
		return false
	}
	if g.ClassificationCeiling != "" && q.ResourceClassification != "" {
		ceiling := classificationOrder[g.ClassificationCeiling]
		actual := classificationOrder[q.ResourceClassification]
		if actual > ceiling {
			return false
		}
	}
	if g.StartsAt != nil && now.Before(*g.StartsAt) {
		return false
	}
	if g.EndsAt != nil && now.After(*g.EndsAt) {
		return false
	}
	return true
}
