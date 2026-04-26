package github

// CR-thread predicate: queries GitHub for the current state of CodeRabbit
// reviews + threads on a PR and answers two boolean questions used by the
// state machine:
//
//   NoOpenCRChangesRequest  — no review by the CR bot is currently in state
//                             CHANGES_REQUESTED that has NOT been dismissed.
//   NoUnresolvedCRThreads   — every review thread started by the CR bot is
//                             marked resolved.
//
// We deliberately scope strictly to the configured CR bot username so other
// reviewers (humans, security bots) don't gate the staged transition.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// PRReviewClient is the minimal interface the predicate evaluator needs from
// the GitHub API. Real implementation is *GitHubAPIClient below; tests
// inject fakes.
type PRReviewClient interface {
	ListReviews(ctx context.Context, owner, repo string, number int) ([]Review, error)
	ListReviewThreads(ctx context.Context, owner, repo string, number int) ([]ReviewThread, error)
}

// Review is the subset of /repos/{o}/{r}/pulls/{n}/reviews we use.
type Review struct {
	ID    int64  `json:"id"`
	State string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, PENDING
	User  struct {
		Login string `json:"login"`
	} `json:"user"`
	SubmittedAt string `json:"submitted_at"`
}

// ReviewThread is the subset returned by the GraphQL reviewThreads query,
// shaped like the REST response we synthesize from it.
type ReviewThread struct {
	IsResolved bool   `json:"isResolved"`
	Author     string `json:"author"` // login of the thread initiator
}

// EvaluatePredicate returns (NoOpenCRChangesRequest, NoUnresolvedCRThreads)
// for the given PR. crBot is the configured CR bot username (e.g.
// "coderabbitai[bot]").
func EvaluatePredicate(ctx context.Context, c PRReviewClient, owner, repo string, number int, crBot string) (bool, bool, error) {
	reviews, err := c.ListReviews(ctx, owner, repo, number)
	if err != nil {
		return false, false, fmt.Errorf("list reviews: %w", err)
	}

	// Walk reviews in order; the latest CR-bot review with a non-DISMISSED
	// terminal state wins for the open-CHANGES check.
	noOpenChanges := true
	var latestCRState string
	for _, r := range reviews {
		if !equalLogin(r.User.Login, crBot) {
			continue
		}
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			latestCRState = r.State
		}
	}
	if latestCRState == "CHANGES_REQUESTED" {
		noOpenChanges = false
	}

	threads, err := c.ListReviewThreads(ctx, owner, repo, number)
	if err != nil {
		return false, false, fmt.Errorf("list review threads: %w", err)
	}
	noUnresolved := true
	for _, t := range threads {
		if !equalLogin(t.Author, crBot) {
			continue
		}
		if !t.IsResolved {
			noUnresolved = false
			break
		}
	}
	return noOpenChanges, noUnresolved, nil
}

func equalLogin(a, b string) bool { return strings.EqualFold(a, b) }

// GitHubAPIClient is a thin REST/GraphQL client that mints fresh installation
// tokens via AppAuth on every request. Stateless from the caller's perspective.
type GitHubAPIClient struct {
	auth       *AppAuth
	httpClient *http.Client

	// installationID is bound at construction; one client per binding.
	installationID int64
}

// NewGitHubAPIClient constructs a client that authenticates as the given
// installation. Cheap to create; reuse per binding if you can.
func NewGitHubAPIClient(auth *AppAuth, installationID int64) *GitHubAPIClient {
	return &GitHubAPIClient{auth: auth, installationID: installationID, httpClient: auth.httpClient}
}

func (c *GitHubAPIClient) doJSON(ctx context.Context, method, urlStr string, body io.Reader, out any) error {
	tok, err := c.auth.InstallationToken(ctx, c.installationID)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "multica-coderabbit-bridge")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github %s %s: status %d: %s", method, urlStr, resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode %s: %w", urlStr, err)
		}
	}
	return nil
}

// ListReviews uses the REST API.
func (c *GitHubAPIClient) ListReviews(ctx context.Context, owner, repo string, number int) ([]Review, error) {
	u := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), number)
	var reviews []Review
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &reviews); err != nil {
		return nil, err
	}
	return reviews, nil
}

// ListReviewThreads uses the GraphQL API since REST does not expose
// isResolved on threads.
func (c *GitHubAPIClient) ListReviewThreads(ctx context.Context, owner, repo string, number int) ([]ReviewThread, error) {
	const query = `query($owner:String!,$repo:String!,$number:Int!){
		repository(owner:$owner,name:$repo){
			pullRequest(number:$number){
				reviewThreads(first:100){
					nodes{
						isResolved
						comments(first:1){ nodes{ author{ login } } }
					}
				}
			}
		}
	}`
	payload := map[string]any{
		"query": query,
		"variables": map[string]any{
			"owner": owner, "repo": repo, "number": number,
		},
	}
	buf, _ := json.Marshal(payload)
	var resp struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
							Comments   struct {
								Nodes []struct {
									Author struct {
										Login string `json:"login"`
									} `json:"author"`
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
	if err := c.doJSON(ctx, http.MethodPost, "https://api.github.com/graphql", strings.NewReader(string(buf)), &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %s", resp.Errors[0].Message)
	}
	out := make([]ReviewThread, 0, len(resp.Data.Repository.PullRequest.ReviewThreads.Nodes))
	for _, n := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
		author := ""
		if len(n.Comments.Nodes) > 0 {
			author = n.Comments.Nodes[0].Author.Login
		}
		out = append(out, ReviewThread{IsResolved: n.IsResolved, Author: author})
	}
	return out, nil
}
