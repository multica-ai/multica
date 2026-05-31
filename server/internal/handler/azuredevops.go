package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Response shapes ──────────────────────────────────────────────────────────

// ADOInstallationResponse is the JSON shape returned by the installation list
// and create endpoints. The encrypted PAT is never included.
type ADOInstallationResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	OrgURL      string  `json:"org_url"`
	DisplayName string  `json:"display_name"`
	// WebhookURL is the full webhook URL the operator pastes into Azure DevOps
	// Service Hooks. Only populated on the creation response so the operator
	// can copy it once; subsequent list responses omit it.
	WebhookURL  *string `json:"webhook_url,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// ADOPullRequestResponse is the wire shape for an ADO PR, parallel to
// GitHubPullRequestResponse so the frontend can treat them uniformly.
type ADOPullRequestResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	Provider       string  `json:"provider"` // always "azure_devops"
	OrgURL         string  `json:"org_url"`
	Project        string  `json:"project"`
	RepoName       string  `json:"repo_name"`
	Number         int32   `json:"number"` // pr_id_ado
	Title          string  `json:"title"`
	State          string  `json:"state"`
	HtmlURL        string  `json:"html_url"`
	Branch         *string `json:"branch"`
	AuthorLogin    *string `json:"author_login"`
	AuthorAvatarURL *string `json:"author_avatar_url"`
	MergedAt       *string `json:"merged_at"`
	ClosedAt       *string `json:"closed_at"`
	PRCreatedAt    string  `json:"pr_created_at"`
	PRUpdatedAt    string  `json:"pr_updated_at"`
	// PolicyStatus is the aggregated gate result: "approved"/"blocked"/"pending"/null.
	PolicyStatus   *string `json:"policy_status"`
	MergeStatus    *string `json:"merge_status"`
	ChecksPassed   int64   `json:"checks_passed"`
	ChecksFailed   int64   `json:"checks_failed"`
	ChecksPending  int64   `json:"checks_pending"`
	ChecksConclusion *string `json:"checks_conclusion"`
}

func adoInstallationToResponse(i db.AdoInstallation, webhookURL *string) ADOInstallationResponse {
	return ADOInstallationResponse{
		ID:          uuidToString(i.ID),
		WorkspaceID: uuidToString(i.WorkspaceID),
		OrgURL:      i.OrgURL,
		DisplayName: i.DisplayName,
		WebhookURL:  webhookURL,
		CreatedAt:   timestampToString(i.CreatedAt),
	}
}

func adoPullRequestToResponse(p db.ListADOPullRequestsByIssueRow) ADOPullRequestResponse {
	conclusion := adoAggregateChecksConclusion(p.ChecksFailed, p.ChecksPassed, p.ChecksPending, p.ChecksTotal)
	return ADOPullRequestResponse{
		ID:              uuidToString(p.ID),
		WorkspaceID:     uuidToString(p.WorkspaceID),
		Provider:        "azure_devops",
		OrgURL:          p.OrgURL,
		Project:         p.Project,
		RepoName:        p.RepoName,
		Number:          p.PrIDAdo,
		Title:           p.Title,
		State:           p.State,
		HtmlURL:         p.HtmlUrl,
		Branch:          textToPtr(p.Branch),
		AuthorLogin:     textToPtr(p.AuthorLogin),
		AuthorAvatarURL: textToPtr(p.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(p.MergedAt),
		ClosedAt:        timestampToPtr(p.ClosedAt),
		PRCreatedAt:     timestampToString(p.PrCreatedAt),
		PRUpdatedAt:     timestampToString(p.PrUpdatedAt),
		PolicyStatus:    textToPtr(p.PolicyStatus),
		MergeStatus:     textToPtr(p.MergeStatus),
		ChecksPassed:    p.ChecksPassed,
		ChecksFailed:    p.ChecksFailed,
		ChecksPending:   p.ChecksPending,
		ChecksConclusion: conclusion,
	}
}

func adoAggregateChecksConclusion(failed, passed, pending, total int64) *string {
	if total == 0 {
		return nil
	}
	var v string
	switch {
	case failed > 0:
		v = "failed"
	case pending > 0:
		v = "pending"
	case passed > 0:
		v = "passed"
	default:
		return nil
	}
	return &v
}

// ── Encryption helpers ───────────────────────────────────────────────────────

// adoEncryptionKey returns the 32-byte key used to encrypt PATs at rest.
// Falls back to a zero key when the env var is absent (dev/test only — the
// zero key means the PAT is AES-encrypted but not secret; a warning is logged).
func adoEncryptionKey() []byte {
	raw := strings.TrimSpace(os.Getenv("ADO_ENCRYPTION_KEY"))
	if raw == "" {
		slog.Warn("ADO_ENCRYPTION_KEY not set — PATs will use a zero key (dev mode only)")
		return make([]byte, 32)
	}
	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		slog.Error("ADO_ENCRYPTION_KEY must be 32 bytes hex-encoded (64 hex chars)")
		return make([]byte, 32)
	}
	return key
}

// encryptPAT encrypts the PAT using AES-256-GCM. Returns nonce || ciphertext.
func encryptPAT(pat string) ([]byte, error) {
	block, err := aes.NewCipher(adoEncryptionKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(pat), nil)
	return sealed, nil
}

// decryptPAT decrypts a PAT encrypted by encryptPAT. The first NonceSize
// bytes of data are the nonce.
func decryptPAT(data []byte) (string, error) {
	block, err := aes.NewCipher(adoEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// ── Webhook URL helpers ──────────────────────────────────────────────────────

// adoWebhookBaseURL returns the public-facing base URL for the Multica server.
func adoWebhookBaseURL() string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_PUBLIC_URL")), "/")
	if base == "" {
		base = "http://localhost:8080"
	}
	return base
}

func buildWebhookURL(secret string) string {
	return adoWebhookBaseURL() + "/api/webhooks/azuredevops?secret=" + secret
}

// ── ADO PAT validation ───────────────────────────────────────────────────────

// validateADOPAT calls the ADO Projects API to confirm the PAT is valid and
// returns the organization display name. Returns an error for invalid/expired PATs.
func validateADOPAT(ctx context.Context, orgURL, pat string) (displayName string, err error) {
	orgURL = strings.TrimRight(orgURL, "/")
	apiURL := orgURL + "/_apis/projects?api-version=7.1&$top=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	// ADO PAT auth: Basic base64(":" + pat)
	encoded := base64.StdEncoding.EncodeToString([]byte(":" + pat))
	req.Header.Set("Authorization", "Basic "+encoded)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ADO API unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", errors.New("invalid or expired PAT — check scopes (needs Code Read)")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ADO API returned %d", resp.StatusCode)
	}
	// Extract org name from URL for the display name.
	parts := strings.Split(strings.TrimPrefix(orgURL, "https://dev.azure.com/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		displayName = parts[0]
	} else {
		displayName = orgURL
	}
	return displayName, nil
}

// ── Installation endpoints ───────────────────────────────────────────────────

type createADOInstallationRequest struct {
	OrgURL string `json:"org_url"`
	PAT    string `json:"pat"`
}

// CreateADOInstallation (POST /api/workspaces/{id}/azuredevops/installations)
// validates the PAT, encrypts it, and persists the installation row.
func (h *Handler) CreateADOInstallation(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req createADOInstallationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.OrgURL = strings.TrimSpace(req.OrgURL)
	req.PAT = strings.TrimSpace(req.PAT)
	if req.OrgURL == "" {
		writeError(w, http.StatusBadRequest, "org_url is required")
		return
	}
	if req.PAT == "" {
		writeError(w, http.StatusBadRequest, "pat is required")
		return
	}

	// Normalize org URL to canonical https://dev.azure.com/{org} form.
	normalized, _, _, _ := normalizeADOURL(req.OrgURL + "/_git/dummy")
	if normalized != "" {
		// strip /{project}/_git/dummy from the normalized URL
		parts := strings.Split(strings.TrimPrefix(normalized, "https://dev.azure.com/"), "/")
		if len(parts) > 0 {
			req.OrgURL = "https://dev.azure.com/" + parts[0]
		}
	} else if !strings.HasPrefix(req.OrgURL, "https://") {
		writeError(w, http.StatusBadRequest, "org_url must be https://dev.azure.com/{org}")
		return
	}

	// Validate PAT against ADO.
	displayName, err := validateADOPAT(r.Context(), req.OrgURL, req.PAT)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "PAT validation failed: "+err.Error())
		return
	}

	// Encrypt PAT.
	encrypted, err := encryptPAT(req.PAT)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt PAT")
		return
	}

	connectedBy := pgtype.UUID{}
	if u, err := parseStrictUUID(userID); err == nil {
		connectedBy = u
	}

	inst, err := h.Queries.CreateADOInstallation(r.Context(), db.CreateADOInstallationParams{
		WorkspaceID:   wsUUID,
		OrgURL:        req.OrgURL,
		DisplayName:   displayName,
		PatEncrypted:  encrypted,
		ConnectedByID: connectedBy,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save installation")
		return
	}

	webhookURL := buildWebhookURL(inst.WebhookSecret)
	resp := adoInstallationToResponse(inst, &webhookURL)
	h.publish(protocol.EventADOInstallationCreated, workspaceID, "system", "", map[string]any{
		"installation": adoInstallationToResponse(inst, nil),
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ListADOInstallations (GET /api/workspaces/{id}/azuredevops/installations)
func (h *Handler) ListADOInstallations(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, _ := middleware.MemberFromContext(r.Context())
	canManage := roleAllowed(member.Role, "owner", "admin")

	rows, err := h.Queries.ListADOInstallationsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installations")
		return
	}
	out := make([]ADOInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, adoInstallationToResponse(row, nil))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out,
		"can_manage":    canManage,
	})
}

// DeleteADOInstallation (DELETE /api/workspaces/{id}/azuredevops/installations/{installationId})
func (h *Handler) DeleteADOInstallation(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	idStr := chi.URLParam(r, "installationId")
	idUUID, ok := parseUUIDOrBadRequest(w, idStr, "installation id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteADOInstallation(r.Context(), db.DeleteADOInstallationParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove installation")
		return
	}
	h.publish(protocol.EventADOInstallationDeleted, workspaceID, "system", "", map[string]any{
		"id": idStr,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ListADOPullRequestsForIssue (GET /api/issues/{id}/ado-pull-requests)
func (h *Handler) ListADOPullRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListADOPullRequestsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list ADO pull requests")
		return
	}
	out := make([]ADOPullRequestResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, adoPullRequestToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pull_requests": out})
}

// ── Webhook ──────────────────────────────────────────────────────────────────

// HandleADOWebhook (POST /api/webhooks/azuredevops?secret={secret})
// receives Azure DevOps Service Hook events. Auth is via the secret query
// parameter which maps to ado_installation.webhook_secret.
func (h *Handler) HandleADOWebhook(w http.ResponseWriter, r *http.Request) {
	secret := strings.TrimSpace(r.URL.Query().Get("secret"))
	if secret == "" {
		writeError(w, http.StatusUnauthorized, "missing webhook secret")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	inst, err := h.Queries.GetADOInstallationByWebhookSecret(r.Context(), secret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "unknown webhook secret")
		} else {
			writeError(w, http.StatusInternalServerError, "webhook secret lookup failed")
		}
		return
	}

	// Dispatch on eventType.
	var envelope struct {
		EventType string          `json:"eventType"`
		Resource  json.RawMessage `json:"resource"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	ctx := r.Context()
	switch envelope.EventType {
	case "git.pullrequest.created", "git.pullrequest.updated":
		h.handleADOPullRequestEvent(ctx, inst, envelope.Resource, envelope.EventType)
	case "git.pullrequest.merged":
		h.handleADOPullRequestMerged(ctx, inst, envelope.Resource)
	case "git.pullrequest.abandoned":
		h.handleADOPullRequestAbandoned(ctx, inst, envelope.Resource)
	case "build.complete":
		h.handleADOBuildComplete(ctx, inst, envelope.Resource)
	// Acknowledge but ignore other event types.
	}
	w.WriteHeader(http.StatusAccepted)
}

// ── ADO event payloads ───────────────────────────────────────────────────────

type adoPullRequestResource struct {
	PullRequestID int32  `json:"pullRequestId"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"` // active/completed/abandoned
	IsDraft       bool   `json:"isDraft"`
	URL           string `json:"url"` // html URL
	SourceRefName string `json:"sourceRefName"` // refs/heads/feature/foo
	MergeStatus   string `json:"mergeStatus"`   // notSet/queued/blocked/succeeded/conflicts/rejectedByPolicy
	CreatedBy     struct {
		DisplayName string `json:"displayName"`
		UniqueName  string `json:"uniqueName"`
		ImageURL    string `json:"imageUrl"`
	} `json:"createdBy"`
	CreationDate string `json:"creationDate"` // RFC3339
	ClosedDate   string `json:"closedDate"`
	Repository   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
	} `json:"repository"`
	Reviewers []struct {
		Vote int `json:"vote"` // 10=approved, 5=approved with suggestions, 0=no vote, -5=waiting, -10=rejected
	} `json:"reviewers"`
}

func (h *Handler) handleADOPullRequestEvent(ctx context.Context, inst db.AdoInstallation, raw json.RawMessage, eventType string) {
	var pr adoPullRequestResource
	if err := json.Unmarshal(raw, &pr); err != nil {
		slog.Warn("ado: bad pull_request payload", "err", err)
		return
	}
	state := deriveADOPRState(pr.Status, pr.IsDraft)
	policyStatus := deriveADOPolicyStatus(pr.MergeStatus, pr.Reviewers)
	branch := strings.TrimPrefix(pr.SourceRefName, "refs/heads/")

	row, err := h.Queries.UpsertADOPullRequest(ctx, db.UpsertADOPullRequestParams{
		WorkspaceID:     inst.WorkspaceID,
		InstallationID:  inst.ID,
		OrgURL:          inst.OrgURL,
		Project:         pr.Repository.Project.Name,
		RepoName:        pr.Repository.Name,
		RepoIDAdo:       pr.Repository.ID,
		PrIDAdo:         pr.PullRequestID,
		Title:           pr.Title,
		State:           state,
		HtmlUrl:         buildADOPRURL(inst.OrgURL, pr.Repository.Project.Name, pr.Repository.Name, pr.PullRequestID),
		Branch:          strToText(branch),
		AuthorLogin:     strToText(pr.CreatedBy.UniqueName),
		AuthorAvatarUrl: strToText(pr.CreatedBy.ImageURL),
		MergedAt:        pgtype.Timestamptz{},
		ClosedAt:        parseADOTime(pr.ClosedDate),
		PrCreatedAt:     parseADOTimeRequired(pr.CreationDate),
		PrUpdatedAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		PolicyStatus:    policyStatus,
		MergeStatus:     strToText(pr.MergeStatus),
	})
	if err != nil {
		slog.Warn("ado: upsert pr failed", "err", err)
		return
	}

	workspaceID := uuidToString(inst.WorkspaceID)
	prefix := h.getIssuePrefix(ctx, inst.WorkspaceID)

	idents := extractIdentifiers(pr.Title, pr.Description, branch)
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(pr.Title, pr.Description) {
		closingIdents[c] = struct{}{}
	}
	preserveCloseIntent := eventType == "git.pullrequest.updated" && (state == "merged" || state == "abandoned")

	reevalIssues := make([]db.Issue, 0, len(idents))
	for _, id := range idents {
		issue, ok := h.lookupIssueByIdentifier(ctx, inst.WorkspaceID, prefix, id)
		if !ok {
			continue
		}
		_, declared := closingIdents[id]
		closeIntent := declared && !preserveCloseIntent
		if err := h.Queries.LinkIssueToADOPullRequest(ctx, db.LinkIssueToADOPullRequestParams{
			IssueID:             issue.ID,
			PullRequestID:       row.ID,
			CloseIntent:         closeIntent,
			LinkedByType:        strToText("system"),
			LinkedByID:          pgtype.UUID{},
			PreserveCloseIntent: preserveCloseIntent,
		}); err != nil {
			slog.Warn("ado: link issue to pr failed", "err", err)
			continue
		}
		reevalIssues = append(reevalIssues, issue)
	}

	h.publish(protocol.EventADOPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request": map[string]any{
			"id":           uuidToString(row.ID),
			"workspace_id": workspaceID,
			"provider":     "azure_devops",
		},
		"linked_issue_ids": issueListToIDs(reevalIssues),
	})
}

func (h *Handler) handleADOPullRequestMerged(ctx context.Context, inst db.AdoInstallation, raw json.RawMessage) {
	var pr adoPullRequestResource
	if err := json.Unmarshal(raw, &pr); err != nil {
		slog.Warn("ado: bad pullrequest merged payload", "err", err)
		return
	}

	row, err := h.Queries.UpsertADOPullRequest(ctx, db.UpsertADOPullRequestParams{
		WorkspaceID:     inst.WorkspaceID,
		InstallationID:  inst.ID,
		OrgURL:          inst.OrgURL,
		Project:         pr.Repository.Project.Name,
		RepoName:        pr.Repository.Name,
		RepoIDAdo:       pr.Repository.ID,
		PrIDAdo:         pr.PullRequestID,
		Title:           pr.Title,
		State:           "merged",
		HtmlUrl:         buildADOPRURL(inst.OrgURL, pr.Repository.Project.Name, pr.Repository.Name, pr.PullRequestID),
		Branch:          strToText(strings.TrimPrefix(pr.SourceRefName, "refs/heads/")),
		AuthorLogin:     strToText(pr.CreatedBy.UniqueName),
		AuthorAvatarUrl: strToText(pr.CreatedBy.ImageURL),
		MergedAt:        pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		ClosedAt:        parseADOTime(pr.ClosedDate),
		PrCreatedAt:     parseADOTimeRequired(pr.CreationDate),
		PrUpdatedAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		PolicyStatus:    pgtype.Text{String: "approved", Valid: true},
		MergeStatus:     strToText("succeeded"),
	})
	if err != nil {
		slog.Warn("ado: upsert merged pr failed", "err", err)
		return
	}

	workspaceID := uuidToString(inst.WorkspaceID)
	prefix := h.getIssuePrefix(ctx, inst.WorkspaceID)
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(pr.Title, pr.Description) {
		closingIdents[c] = struct{}{}
	}

	issues, err := h.Queries.ListIssueIDsForADOPullRequest(ctx, row.ID)
	if err == nil {
		for _, issueID := range issues {
			issue, err := h.Queries.GetIssue(ctx, issueID)
			if err != nil {
				continue
			}
			// Link close_intent based on the PR description at merge time.
			// PreserveCloseIntent=true forces the SQL ON CONFLICT to write the
			// newly-computed value (EXCLUDED.close_intent) rather than keeping
			// the existing row value.
			ident := strings.ToUpper(prefix) + "-" + fmt.Sprint(issue.Number)
			_, hasCloseIntent := closingIdents[ident]
			_ = h.Queries.LinkIssueToADOPullRequest(ctx, db.LinkIssueToADOPullRequestParams{
				IssueID:             issue.ID,
				PullRequestID:       row.ID,
				CloseIntent:         hasCloseIntent,
				LinkedByType:        strToText("system"),
				LinkedByID:          pgtype.UUID{},
				PreserveCloseIntent: true,
			})
		}
	}

	// Re-evaluate auto-advance for linked issues.
	if linkedIssues, err := h.Queries.ListIssueIDsForADOPullRequest(ctx, row.ID); err == nil {
		for _, issueID := range linkedIssues {
			issue, err := h.Queries.GetIssue(ctx, issueID)
			if err != nil || issue.Status == "done" || issue.Status == "cancelled" {
				continue
			}
			counts, err := h.Queries.GetADOIssuePullRequestCloseAggregate(ctx, issue.ID)
			if err != nil {
				continue
			}
			if counts.OpenCount == 0 && counts.MergedWithCloseIntentCount > 0 {
				h.advanceIssueToDone(ctx, issue, workspaceID)
			}
		}
	}

	h.publish(protocol.EventADOPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request": map[string]any{"id": uuidToString(row.ID), "state": "merged"},
	})
}

func (h *Handler) handleADOPullRequestAbandoned(ctx context.Context, inst db.AdoInstallation, raw json.RawMessage) {
	var pr adoPullRequestResource
	if err := json.Unmarshal(raw, &pr); err != nil {
		slog.Warn("ado: bad pullrequest abandoned payload", "err", err)
		return
	}
	row, err := h.Queries.UpsertADOPullRequest(ctx, db.UpsertADOPullRequestParams{
		WorkspaceID:     inst.WorkspaceID,
		InstallationID:  inst.ID,
		OrgURL:          inst.OrgURL,
		Project:         pr.Repository.Project.Name,
		RepoName:        pr.Repository.Name,
		RepoIDAdo:       pr.Repository.ID,
		PrIDAdo:         pr.PullRequestID,
		Title:           pr.Title,
		State:           "abandoned",
		HtmlUrl:         buildADOPRURL(inst.OrgURL, pr.Repository.Project.Name, pr.Repository.Name, pr.PullRequestID),
		Branch:          strToText(strings.TrimPrefix(pr.SourceRefName, "refs/heads/")),
		AuthorLogin:     strToText(pr.CreatedBy.UniqueName),
		AuthorAvatarUrl: strToText(pr.CreatedBy.ImageURL),
		MergedAt:        pgtype.Timestamptz{},
		ClosedAt:        pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		PrCreatedAt:     parseADOTimeRequired(pr.CreationDate),
		PrUpdatedAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		PolicyStatus:    pgtype.Text{},
		MergeStatus:     strToText(pr.MergeStatus),
	})
	if err != nil {
		slog.Warn("ado: upsert abandoned pr failed", "err", err)
		return
	}
	h.publish(protocol.EventADOPullRequestUpdated, uuidToString(inst.WorkspaceID), "system", "", map[string]any{
		"pull_request": map[string]any{"id": uuidToString(row.ID), "state": "abandoned"},
	})
}

type adoBuildResource struct {
	ID         int64  `json:"id"`
	BuildNumber string `json:"buildNumber"`
	Status     string `json:"status"`     // completed/inProgress/notStarted
	Result     string `json:"result"`     // succeeded/failed/canceled/partiallySucceeded
	FinishTime string `json:"finishTime"`
	Definition struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"definition"`
	Repository struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"repository"`
	TriggerInfo struct {
		PullRequestID string `json:"pr.id"` // set when triggered by a PR policy
	} `json:"triggerInfo"`
	Project struct {
		Name string `json:"name"`
	} `json:"project"`
}

func (h *Handler) handleADOBuildComplete(ctx context.Context, inst db.AdoInstallation, raw json.RawMessage) {
	var build adoBuildResource
	if err := json.Unmarshal(raw, &build); err != nil {
		slog.Warn("ado: bad build.complete payload", "err", err)
		return
	}
	if build.TriggerInfo.PullRequestID == "" {
		return // build not triggered by a PR policy — nothing to link
	}

	// Find the PR this build belongs to.
	var prIDInt int32
	if _, err := fmt.Sscanf(build.TriggerInfo.PullRequestID, "%d", &prIDInt); err != nil {
		return
	}
	pr, err := h.Queries.GetADOPullRequest(ctx, db.GetADOPullRequestParams{
		WorkspaceID: inst.WorkspaceID,
		OrgURL:      inst.OrgURL,
		Project:     build.Project.Name,
		RepoName:    build.Repository.Name,
		PrIDAdo:     prIDInt,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("ado: lookup pr for build failed", "err", err)
		}
		return
	}

	updatedAt := parseADOTimeRequired(build.FinishTime)
	if err := h.Queries.UpsertADOBuildCheck(ctx, db.UpsertADOBuildCheckParams{
		PrID:           pr.ID,
		BuildID:        build.ID,
		DefinitionID:   int32(build.Definition.ID),
		DefinitionName: build.Definition.Name,
		Conclusion:     strToText(build.Result),
		Status:         build.Status,
		UpdatedAt:      updatedAt,
	}); err != nil {
		slog.Warn("ado: upsert build check failed", "err", err)
		return
	}

	workspaceID := uuidToString(inst.WorkspaceID)
	issues, _ := h.Queries.ListIssueIDsForADOPullRequest(ctx, pr.ID)
	linkedIDs := make([]string, 0, len(issues))
	for _, id := range issues {
		linkedIDs = append(linkedIDs, uuidToString(id))
	}
	h.publish(protocol.EventADOPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request":     map[string]any{"id": uuidToString(pr.ID)},
		"linked_issue_ids": linkedIDs,
	})
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// deriveADOPRState maps ADO status to Multica PR state.
func deriveADOPRState(status string, isDraft bool) string {
	switch status {
	case "completed":
		return "merged"
	case "abandoned":
		return "abandoned"
	}
	if isDraft {
		return "draft"
	}
	return "open"
}

// deriveADOPolicyStatus aggregates the PR mergeStatus and reviewer votes into
// Multica's three-value policy_status field.
//
//   - "approved"  → merge gate is clear (succeeded / noVote with no rejections)
//   - "blocked"   → at least one gate is rejected (rejectedByPolicy / any -10 vote)
//   - "pending"   → gates are still running (queued)
//   - nil         → unknown / not applicable
func deriveADOPolicyStatus(mergeStatus string, reviewers []struct {
	Vote int `json:"vote"`
}) pgtype.Text {
	// Check reviewer votes first: a -10 vote always blocks.
	for _, r := range reviewers {
		if r.Vote == -10 {
			return pgtype.Text{String: "blocked", Valid: true}
		}
	}
	switch mergeStatus {
	case "succeeded", "noVote":
		return pgtype.Text{String: "approved", Valid: true}
	case "blocked", "rejectedByPolicy":
		return pgtype.Text{String: "blocked", Valid: true}
	case "queued":
		return pgtype.Text{String: "pending", Valid: true}
	}
	return pgtype.Text{}
}

func buildADOPRURL(orgURL, project, repoName string, prID int32) string {
	return fmt.Sprintf("%s/%s/_git/%s/pullrequest/%d", orgURL, project, repoName, prID)
}

func parseADOTime(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// ADO sometimes uses a non-standard format
		t, err = time.Parse("2006-01-02T15:04:05.999999999Z07:00", s)
		if err != nil {
			return pgtype.Timestamptz{}
		}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func parseADOTimeRequired(s string) pgtype.Timestamptz {
	t := parseADOTime(s)
	if !t.Valid {
		return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	return t
}

func issueListToIDs(issues []db.Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, i := range issues {
		ids = append(ids, uuidToString(i.ID))
	}
	return ids
}
