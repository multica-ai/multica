package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/forgejo"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fjPullRequestPayload is the subset of the Forgejo (Gitea-compatible)
// pull_request webhook payload we mirror. Forgejo's shape is close to but not
// identical to GitHub's: the actor login lives under user.username, the repo
// owner under repository.owner.username, and there is no installation/app
// envelope (the connection is identified by the webhook URL path instead).
type fjPullRequestPayload struct {
	Action      string `json:"action"`
	Number      int32  `json:"number"`
	PullRequest struct {
		Number       int32  `json:"number"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		State        string `json:"state"`
		Merged       bool   `json:"merged"`
		Draft        bool   `json:"draft"`
		HTMLURL      string `json:"html_url"`
		Additions    int32  `json:"additions"`
		Deletions    int32  `json:"deletions"`
		ChangedFiles int32  `json:"changed_files"`
		MergedAt     string `json:"merged_at"`
		ClosedAt     string `json:"closed_at"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		User         struct {
			Login     string `json:"login"`
			UserName  string `json:"username"`
			AvatarURL string `json:"avatar_url"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
			Sha string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login    string `json:"login"`
			UserName string `json:"username"`
		} `json:"owner"`
	} `json:"repository"`
}

// HandleForgejoWebhook (POST /api/webhooks/forgejo/{connectionId}) verifies the
// per-connection HMAC signature and mirrors pull_request events. The connection
// id in the path selects the workspace + decryption secret; unlike GitHub there
// is no shared app secret, so each connection has its own URL and secret.
func (h *Handler) HandleForgejoWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.isForgejoConfigured() {
		writeError(w, http.StatusServiceUnavailable, "forgejo webhooks not configured")
		return
	}
	connID := chi.URLParam(r, "connectionId")
	connUUID, ok := parseUUIDOrBadRequest(w, connID, "connection id")
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	conn, err := h.Queries.GetForgejoConnectionByID(r.Context(), connUUID)
	if err != nil {
		// Unknown connection: acknowledge without leaking existence, but do not
		// process. 404 is acceptable since the id is a server-minted secret-ish
		// handle and the caller is the forge, not a browser.
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("forgejo: lookup connection failed", "err", err)
		}
		writeError(w, http.StatusNotFound, "unknown connection")
		return
	}

	secret, err := h.openForgejoSecret(conn.WebhookSecretEncrypted)
	if err != nil {
		slog.Error("forgejo: decrypt webhook secret failed", "err", err)
		writeError(w, http.StatusInternalServerError, "secret error")
		return
	}
	if !forgejo.VerifyWebhookSignature(secret, r.Header.Get("X-Gitea-Signature"), body) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Forgejo sends the event type in X-Gitea-Event (mirrored as
	// X-GitHub-Event for compatibility). Only pull_request is modelled today.
	event := r.Header.Get("X-Gitea-Event")
	if event == "" {
		event = r.Header.Get("X-GitHub-Event")
	}
	switch event {
	case "pull_request":
		h.handleForgejoPullRequestEvent(r.Context(), conn, body)
	default:
		// Acknowledge unmodelled events so Forgejo doesn't flag the hook.
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handleForgejoPullRequestEvent(ctx context.Context, conn db.ForgejoConnection, body []byte) {
	var p fjPullRequestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Warn("forgejo: bad pull_request payload", "err", err)
		return
	}

	owner := forgejoRepoOwner(p)
	name := p.Repository.Name
	if owner == "" || name == "" || p.PullRequest.Number == 0 {
		slog.Warn("forgejo: pull_request payload missing repo identity")
		return
	}

	state := derivePRState(p.PullRequest.State, p.PullRequest.Draft, p.PullRequest.Merged)
	authorLogin := coalesce(p.PullRequest.User.UserName, p.PullRequest.User.Login)

	pr, err := h.Queries.UpsertForgejoPullRequest(ctx, db.UpsertForgejoPullRequestParams{
		WorkspaceID:     conn.WorkspaceID,
		ConnectionID:    conn.ID,
		RepoOwner:       owner,
		RepoName:        name,
		PrNumber:        p.PullRequest.Number,
		Title:           p.PullRequest.Title,
		State:           state,
		HtmlUrl:         p.PullRequest.HTMLURL,
		Branch:          ptrToText(strPtrOrNil(p.PullRequest.Head.Ref)),
		AuthorLogin:     ptrToText(strPtrOrNil(authorLogin)),
		AuthorAvatarUrl: ptrToText(strPtrOrNil(p.PullRequest.User.AvatarURL)),
		MergedAt:        parseGHTime(p.PullRequest.MergedAt),
		ClosedAt:        parseGHTime(p.PullRequest.ClosedAt),
		PrCreatedAt:     parseGHTimeRequired(p.PullRequest.CreatedAt),
		PrUpdatedAt:     parseGHTimeRequired(p.PullRequest.UpdatedAt),
		Additions:       p.PullRequest.Additions,
		Deletions:       p.PullRequest.Deletions,
		ChangedFiles:    p.PullRequest.ChangedFiles,
	})
	if err != nil {
		slog.Warn("forgejo: upsert pr failed", "err", err)
		return
	}

	workspaceID := uuidToString(conn.WorkspaceID)
	resp := forgejoPullRequestToResponse(pr)

	// Auto-link to issues by identifiers in title/body/branch. Connecting a
	// Forgejo instance is itself the opt-in, so there is no separate per-
	// workspace auto-link flag the way GitHub has. The issue-side machinery
	// (identifier extraction, lookup, auto-advance) is shared with GitHub.
	linkedIssueIDs := make([]string, 0)
	idents := extractIdentifiers(p.PullRequest.Title, p.PullRequest.Body, p.PullRequest.Head.Ref)
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(p.PullRequest.Title, p.PullRequest.Body) {
		closingIdents[c] = struct{}{}
	}
	// Once a terminal event has been delivered, later edits must not rewrite
	// the merge-time close decision. Forgejo delivers the terminal event as
	// action "closed" (with pull_request.merged distinguishing merge from
	// plain close); some versions also emit "merged", so treat both as
	// terminal to avoid freezing close_intent one event too early.
	terminalAction := p.Action == "closed" || p.Action == "merged"
	preserveCloseIntent := !terminalAction && (state == "merged" || state == "closed")
	prefix := h.getIssuePrefix(ctx, conn.WorkspaceID)
	reevalIssues := make([]db.Issue, 0, len(idents))
	for _, id := range idents {
		issue, ok := h.lookupIssueByIdentifier(ctx, conn.WorkspaceID, prefix, id)
		if !ok {
			continue
		}
		_, declared := closingIdents[id]
		closeIntent := declared && !preserveCloseIntent
		if err := h.Queries.LinkIssueToForgejoPullRequest(ctx, db.LinkIssueToForgejoPullRequestParams{
			IssueID:             issue.ID,
			PullRequestID:       pr.ID,
			CloseIntent:         closeIntent,
			PreserveCloseIntent: preserveCloseIntent,
			LinkedByType:        strToText("system"),
			LinkedByID:          pgtype.UUID{},
		}); err != nil {
			slog.Warn("forgejo: link failed", "err", err)
			continue
		}
		linkedIssueIDs = append(linkedIssueIDs, uuidToString(issue.ID))
		reevalIssues = append(reevalIssues, issue)
	}

	// On a terminal PR event, auto-advance any linked issue once no linked PR
	// is still open/draft and at least one merged PR declared close intent.
	if state == "merged" || state == "closed" {
		for _, issue := range reevalIssues {
			if issue.Status == "done" || issue.Status == "cancelled" {
				continue
			}
			counts, err := h.Queries.GetForgejoIssuePullRequestCloseAggregate(ctx, issue.ID)
			if err != nil {
				slog.Warn("forgejo: count linked pr states failed", "err", err, "issue_id", uuidToString(issue.ID))
				continue
			}
			if counts.OpenCount == 0 && counts.MergedWithCloseIntentCount > 0 {
				h.advanceIssueToDone(ctx, issue, workspaceID)
			}
		}
	}

	h.publish(protocol.EventPullRequestUpdated, workspaceID, "system", "", map[string]any{
		"pull_request":     resp,
		"linked_issue_ids": linkedIssueIDs,
	})
}

// forgejoRepoOwner extracts the repo owner login, tolerating the field-name
// variance between Forgejo versions (owner.username vs owner.login) and
// falling back to the owner segment of full_name ("owner/repo").
func forgejoRepoOwner(p fjPullRequestPayload) string {
	if o := coalesce(p.Repository.Owner.UserName, p.Repository.Owner.Login); o != "" {
		return o
	}
	if i := strings.Index(p.Repository.FullName, "/"); i > 0 {
		return p.Repository.FullName[:i]
	}
	return ""
}

// openForgejoSecret base64-decodes and decrypts a stored secret column.
func (h *Handler) openForgejoSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	plaintext, err := h.ForgejoSecretBox.Open(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
