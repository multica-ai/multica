package github

// Review-thread mutation actions used by the dev agent's fixing loop.
//
// In step 1 we mirrored CR review threads into our local issue_review_thread
// table. In step 2 the state machine drives in_review → fixing whenever a CR
// review lands with at least one unresolved local thread. Step 3 (this file)
// gives the dev agent a way to actually walk those threads in the fixing
// loop and resolve them on GitHub:
//
//   1. ReplyToReviewThread — appends a reply comment under an existing
//      review thread via the GraphQL `addPullRequestReviewThreadReply`
//      mutation.
//   2. ResolveReviewThread — marks the thread resolved on GitHub via the
//      GraphQL `resolveReviewThread` mutation, and mirrors the local
//      issue_review_thread row to state='resolved' immediately so the
//      state machine sees the new count without waiting for the inbound
//      webhook (which still arrives and is treated as a no-op due to the
//      idempotent state setter).
//
// All actions are scoped to a single binding (and hence a single
// installation token). Callers are responsible for resolving the issue's
// binding before calling — typically via Queries.GetRepoBindingByRepo on
// the issue.pr_repo column.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ReviewActions is a small service that wraps the GraphQL mutations the
// dev agent needs while walking review threads in the fixing loop.
//
// Construct one per-process via NewReviewActionsFromEnv (re-uses the same
// GitHub App auth as WebhookHandler) and reuse it across requests.
type ReviewActions struct {
	Queries *db.Queries
	Auth    *AppAuth

	// NewClient overrides client construction in tests. Production code
	// leaves this nil and uses NewGitHubAPIClient.
	NewClient func(installationID int64) *GitHubAPIClient
}

// NewReviewActionsFromEnv mirrors NewWebhookHandlerFromEnv: it loads
// GITHUB_APP_ID + GITHUB_APP_PRIVATE_KEY_PATH and constructs the auth.
func NewReviewActionsFromEnv(queries *db.Queries) (*ReviewActions, error) {
	auth, err := NewAppAuthFromEnv()
	if err != nil {
		return nil, err
	}
	return &ReviewActions{Queries: queries, Auth: auth}, nil
}

func (a *ReviewActions) client(installationID int64) *GitHubAPIClient {
	if a.NewClient != nil {
		return a.NewClient(installationID)
	}
	return NewGitHubAPIClient(a.Auth, installationID)
}

// ---------------------------------------------------------------------------
// Reply
// ---------------------------------------------------------------------------

// ReplyResult captures what GitHub returned after posting a reply on a
// review thread. We surface it primarily so the CLI (and tests) can
// verify the reply landed.
type ReplyResult struct {
	CommentID  int64  `json:"comment_id"`
	CommentURL string `json:"comment_url"`
}

// ReplyToReviewThread posts a reply on the given thread.
//
// thread is the local issue_review_thread row. We use its
// gh_thread_node_id to address the reply target on GitHub. The first
// comment of the thread (which we already have keyed on gh_comment_id)
// is the reply parent for the REST fallback, but GraphQL takes the
// thread node_id directly.
//
// CR observes the reply and, importantly, does NOT consider it a
// "resolution" — that requires the separate resolveReviewThread call.
// Callers should typically reply first (with the dev agent's
// explanation/fix summary) and then resolve.
func (a *ReviewActions) ReplyToReviewThread(ctx context.Context, binding db.WorkspaceRepoBinding, thread db.IssueReviewThread, body string) (*ReplyResult, error) {
	if !thread.GhThreadNodeID.Valid || thread.GhThreadNodeID.String == "" {
		return nil, errors.New("thread has no gh_thread_node_id; cannot post reply via GraphQL")
	}
	if body == "" {
		return nil, errors.New("reply body is empty")
	}
	c := a.client(binding.InstallationID)

	const mutation = `mutation($thread:ID!, $body:String!) {
	  addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $thread, body: $body}) {
	    comment { databaseId url }
	  }
	}`
	payload := map[string]any{
		"query": mutation,
		"variables": map[string]any{
			"thread": thread.GhThreadNodeID.String,
			"body":   body,
		},
	}
	buf, _ := json.Marshal(payload)

	var resp struct {
		Data struct {
			AddPullRequestReviewThreadReply struct {
				Comment struct {
					DatabaseID int64  `json:"databaseId"`
					URL        string `json:"url"`
				} `json:"comment"`
			} `json:"addPullRequestReviewThreadReply"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(buf), &resp); err != nil {
		return nil, fmt.Errorf("addPullRequestReviewThreadReply: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}
	return &ReplyResult{
		CommentID:  resp.Data.AddPullRequestReviewThreadReply.Comment.DatabaseID,
		CommentURL: resp.Data.AddPullRequestReviewThreadReply.Comment.URL,
	}, nil
}

// ---------------------------------------------------------------------------
// Resolve
// ---------------------------------------------------------------------------

// ResolveResult captures the post-resolve state.
type ResolveResult struct {
	ThreadNodeID string `json:"thread_node_id"`
	Resolved     bool   `json:"resolved"`
}

// ResolveReviewThread marks the thread resolved on GitHub AND mirrors the
// state to our local issue_review_thread rows so the state machine sees
// the count drop immediately.
//
// agentID, when non-zero, is stamped onto resolved_by_agent so audit
// logs can attribute the resolution. Pass the dev agent's UUID
// (Amelia's) when the call originates from the fixing loop.
func (a *ReviewActions) ResolveReviewThread(ctx context.Context, binding db.WorkspaceRepoBinding, thread db.IssueReviewThread, agentID pgtype.UUID) (*ResolveResult, error) {
	if !thread.GhThreadNodeID.Valid || thread.GhThreadNodeID.String == "" {
		return nil, errors.New("thread has no gh_thread_node_id; cannot resolve via GraphQL")
	}
	c := a.client(binding.InstallationID)

	const mutation = `mutation($thread:ID!) {
	  resolveReviewThread(input: {threadId: $thread}) {
	    thread { id isResolved }
	  }
	}`
	payload := map[string]any{
		"query":     mutation,
		"variables": map[string]any{"thread": thread.GhThreadNodeID.String},
	}
	buf, _ := json.Marshal(payload)

	var resp struct {
		Data struct {
			ResolveReviewThread struct {
				Thread struct {
					ID         string `json:"id"`
					IsResolved bool   `json:"isResolved"`
				} `json:"thread"`
			} `json:"resolveReviewThread"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(buf), &resp); err != nil {
		return nil, fmt.Errorf("resolveReviewThread: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}

	// Mirror locally. The webhook-driven SetReviewThreadStateByThreadNodeID
	// path will fire later for the same delivery and is idempotent — it
	// also writes state='resolved', so the second write is a no-op.
	if _, err := a.Queries.SetReviewThreadStateByThreadNodeID(ctx, db.SetReviewThreadStateByThreadNodeIDParams{
		GhThreadNodeID: thread.GhThreadNodeID,
		State:          "resolved",
		AgentID:        agentID,
	}); err != nil {
		// Log via error path but don't fail the API call — GitHub already
		// accepted the resolve. The webhook redelivery (if any) will heal
		// the local row.
		return &ResolveResult{
			ThreadNodeID: resp.Data.ResolveReviewThread.Thread.ID,
			Resolved:     resp.Data.ResolveReviewThread.Thread.IsResolved,
		}, fmt.Errorf("resolved on github but local mirror failed: %w", err)
	}

	return &ResolveResult{
		ThreadNodeID: resp.Data.ResolveReviewThread.Thread.ID,
		Resolved:     resp.Data.ResolveReviewThread.Thread.IsResolved,
	}, nil
}

// ---------------------------------------------------------------------------
// Backfill
// ---------------------------------------------------------------------------

// BackfillResult is what BackfillThreadNodeIDs returns to the caller so the
// CLI / HTTP handler can report progress.
type BackfillResult struct {
	ThreadsFetched int `json:"threads_fetched"`
	RowsUpdated    int `json:"rows_updated"`
}

// BackfillThreadNodeIDs walks GitHub's GraphQL pullRequest.reviewThreads
// and writes the thread node_id onto local issue_review_thread rows that
// only know the comment databaseId so far.
//
// We need this because the `pull_request_review_comment` webhook payload
// does NOT include the parent thread's node_id — that only arrives in
// `pull_request_review_thread` deliveries (which fire on resolve/unresolve
// but not on creation). Without the node_id we cannot reply or resolve
// via GraphQL.
//
// The function is idempotent: rows that already have a node_id are
// untouched (the SQL guards on NULL/empty), and it tolerates threads
// whose first comment isn't mirrored locally.
func (a *ReviewActions) BackfillThreadNodeIDs(ctx context.Context, binding db.WorkspaceRepoBinding, prNumber int32) (*BackfillResult, error) {
	owner, name, err := splitRepoFullName(binding.RepoFullName)
	if err != nil {
		return nil, err
	}
	c := a.client(binding.InstallationID)

	const query = `query($owner:String!,$name:String!,$pr:Int!,$cursor:String) {
	  repository(owner:$owner,name:$name) {
	    pullRequest(number:$pr) {
	      reviewThreads(first:100, after:$cursor) {
	        pageInfo { hasNextPage endCursor }
	        nodes {
	          id
	          comments(first:100) { nodes { databaseId } }
	        }
	      }
	    }
	  }
	}`

	res := &BackfillResult{}
	var cursor *string
	for {
		vars := map[string]any{
			"owner": owner,
			"name":  name,
			"pr":    int(prNumber),
		}
		if cursor != nil {
			vars["cursor"] = *cursor
		}
		payload := map[string]any{"query": query, "variables": vars}
		buf, _ := json.Marshal(payload)

		var resp struct {
			Data struct {
				Repository struct {
					PullRequest struct {
						ReviewThreads struct {
							PageInfo struct {
								HasNextPage bool   `json:"hasNextPage"`
								EndCursor   string `json:"endCursor"`
							} `json:"pageInfo"`
							Nodes []struct {
								ID       string `json:"id"`
								Comments struct {
									Nodes []struct {
										DatabaseID int64 `json:"databaseId"`
									} `json:"nodes"`
								} `json:"comments"`
							} `json:"nodes"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := c.doJSON(ctx, http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(buf), &resp); err != nil {
			return res, fmt.Errorf("reviewThreads page: %w", err)
		}
		if len(resp.Errors) > 0 {
			return res, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
		}

		threads := resp.Data.Repository.PullRequest.ReviewThreads
		res.ThreadsFetched += len(threads.Nodes)
		for _, t := range threads.Nodes {
			for _, cm := range t.Comments.Nodes {
				if cm.DatabaseID == 0 || t.ID == "" {
					continue
				}
				n, err := a.Queries.BackfillReviewThreadNodeID(ctx, db.BackfillReviewThreadNodeIDParams{
					GhCommentID:    cm.DatabaseID,
					GhThreadNodeID: pgtype.Text{String: t.ID, Valid: true},
				})
				if err != nil {
					return res, fmt.Errorf("backfill update for comment %d: %w", cm.DatabaseID, err)
				}
				res.RowsUpdated += int(n)
			}
		}

		if !threads.PageInfo.HasNextPage {
			break
		}
		end := threads.PageInfo.EndCursor
		cursor = &end
	}
	return res, nil
}

// splitRepoFullName splits "owner/name" into (owner, name). The DB
// constraint workspace_repo_binding_repo_format guarantees the format,
// but we still defend against drift.
func splitRepoFullName(full string) (string, string, error) {
	for i := 0; i < len(full); i++ {
		if full[i] == '/' {
			return full[:i], full[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid repo_full_name %q (expected owner/name)", full)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// DefaultActionTimeout is the per-call timeout the HTTP handlers use when
// no caller context deadline is set.
const DefaultActionTimeout = 30 * time.Second
