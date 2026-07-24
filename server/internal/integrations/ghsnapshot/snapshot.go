package ghsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// prSnapshotQuery is the single GraphQL query behind the whole feature. It
// returns, in one round trip (Elon measured cost=1):
//   - headRefOid: the head the snapshot describes (pins the anti-stale write);
//   - mergeable: MERGEABLE / CONFLICTING / UNKNOWN — answers "is there a
//     conflict" only;
//   - mergeStateStatus: CLEAN / DIRTY / BLOCKED / BEHIND / UNSTABLE / ... —
//     "Ready to merge" is derived ONLY from CLEAN;
//   - statusCheckRollup: the overall CI verdict plus every check/status context.
//
// $cursor paginates statusCheckRollup.contexts; the caller loops until
// hasNextPage is false (acceptance criterion 2 — never assume <100 contexts).
const prSnapshotQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      headRefOid
      mergeable
      mergeStateStatus
      commits(last:1){nodes{commit{
        statusCheckRollup{
          state
          contexts(first:100,after:$cursor){
            pageInfo{hasNextPage endCursor}
            nodes{
              __typename
              ... on CheckRun{name status conclusion detailsUrl}
              ... on StatusContext{context state targetUrl}
            }
          }
        }
      }}}
    }
  }
}`

// CheckContext is one normalized check for a PR head. Both GraphQL CheckRun and
// StatusContext contexts are flattened into this shape (see normalizeNode), so
// downstream storage and aggregation are uniform.
type CheckContext struct {
	Name string
	// Status is the normalized lifecycle: "queued", "in_progress", or
	// "completed".
	Status string
	// Conclusion is the normalized result: "success", "failure", "neutral",
	// "cancelled", "skipped", "timed_out", "action_required", "startup_failure",
	// "stale", or "error"; empty while the check is still running.
	Conclusion      string
	DetailsURL      string
	IsStatusContext bool
}

// PRSnapshot is the atomic unit written per fetch. It mirrors exactly what the
// API returned — no incremental inference.
type PRSnapshot struct {
	HeadSHA          string
	Mergeable        string // MERGEABLE / CONFLICTING / UNKNOWN (raw enum)
	MergeStateStatus string // CLEAN / DIRTY / BLOCKED / BEHIND / UNSTABLE / ... (raw enum)
	// RollupState is statusCheckRollup.state (SUCCESS/FAILURE/PENDING/ERROR/
	// EXPECTED). Empty ONLY when HasChecks is false.
	RollupState string
	// HasChecks is false when statusCheckRollup was null — GitHub reports "no
	// checks have been created for this commit yet". This must NEVER be rendered
	// as passed (acceptance criterion 5).
	HasChecks bool
	Contexts  []CheckContext
}

// Decided reports whether the snapshot has settled: no check is still running
// and mergeability is known. An undecided snapshot on an open PR drives the
// bounded chase-window re-fetch (refresh.go).
func (s *PRSnapshot) Decided() bool {
	if s.Mergeable == "UNKNOWN" || s.Mergeable == "" {
		return false
	}
	if s.HasChecks {
		switch s.RollupState {
		case "PENDING", "EXPECTED", "":
			return false
		}
	}
	for _, c := range s.Contexts {
		if c.Status != "completed" {
			return false
		}
	}
	return true
}

type graphqlRollup struct {
	State    string `json:"state"`
	Contexts struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []json.RawMessage `json:"nodes"`
	} `json:"contexts"`
}

type graphqlPullRequest struct {
	HeadRefOid       string `json:"headRefOid"`
	Mergeable        string `json:"mergeable"`
	MergeStateStatus string `json:"mergeStateStatus"`
	Commits          struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *graphqlRollup `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

func (pr *graphqlPullRequest) rollup() *graphqlRollup {
	if len(pr.Commits.Nodes) == 0 {
		return nil
	}
	return pr.Commits.Nodes[0].Commit.StatusCheckRollup
}

type graphqlPRData struct {
	Repository struct {
		PullRequest *graphqlPullRequest `json:"pullRequest"`
	} `json:"repository"`
}

const maxSnapshotContextPages = 100

// FetchPRSnapshot runs prSnapshotQuery, paginating statusCheckRollup.contexts
// to completion, and returns the normalized snapshot. A nil rollup yields
// HasChecks=false with no contexts.
func FetchPRSnapshot(ctx context.Context, c *Client, installationID int64, owner, repo string, number int32) (*PRSnapshot, error) {
	if !c.Enabled() {
		return nil, errors.New("ghsnapshot: client not configured")
	}
	snap := &PRSnapshot{}
	cursor := ""
	// Guard against a pathological cursor loop; 100 pages = 10k contexts, far
	// beyond any real PR.
	for page := 0; page < maxSnapshotContextPages; page++ {
		vars := map[string]any{"owner": owner, "repo": repo, "number": number}
		if cursor != "" {
			vars["cursor"] = cursor
		} else {
			vars["cursor"] = nil
		}
		data, err := c.graphQL(ctx, installationID, prSnapshotQuery, vars)
		if err != nil {
			return nil, err
		}
		var parsed graphqlPRData
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, errors.New("ghsnapshot: malformed pull request data")
		}
		pr := parsed.Repository.PullRequest
		if pr == nil {
			return nil, errors.New("ghsnapshot: pull request not found")
		}
		if page == 0 {
			snap.HeadSHA = pr.HeadRefOid
			snap.Mergeable = pr.Mergeable
			snap.MergeStateStatus = pr.MergeStateStatus
		} else if pr.HeadRefOid != snap.HeadSHA {
			// Every page re-reads the PR's latest commit. If a synchronize
			// event advances the head while pagination is in progress, mixing
			// those pages would label new-head contexts as the old head.
			return nil, errors.New("ghsnapshot: pull request head changed during pagination")
		}
		rollup := pr.rollup()
		if rollup == nil {
			// statusCheckRollup is null → no checks yet. Nothing to paginate.
			if page > 0 {
				return nil, errors.New("ghsnapshot: check rollup changed during pagination")
			}
			return snap, nil
		}
		snap.HasChecks = true
		snap.RollupState = rollup.State
		for _, raw := range rollup.Contexts.Nodes {
			if cc, ok := normalizeNode(raw); ok {
				snap.Contexts = append(snap.Contexts, cc)
			}
		}
		if !rollup.Contexts.PageInfo.HasNextPage {
			return snap, nil
		}
		nextCursor := rollup.Contexts.PageInfo.EndCursor
		if nextCursor == "" || nextCursor == cursor {
			return nil, errors.New("ghsnapshot: invalid check-context pagination cursor")
		}
		if page == maxSnapshotContextPages-1 {
			return nil, errors.New("ghsnapshot: check-context pagination exceeds page limit")
		}
		cursor = nextCursor
	}
	return nil, errors.New("ghsnapshot: check-context pagination exceeds page limit")
}

// normalizeNode flattens one GraphQL union node (CheckRun or StatusContext)
// into a CheckContext. CheckRun statuses/conclusions map to lowercase; legacy
// StatusContext states map onto the same lifecycle so aggregation is uniform:
//
//	SUCCESS  → completed / success
//	FAILURE  → completed / failure
//	ERROR    → completed / error
//	PENDING  → in_progress / (running)
//	EXPECTED → queued / (running)
func normalizeNode(raw json.RawMessage) (CheckContext, bool) {
	var probe struct {
		Typename string `json:"__typename"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return CheckContext{}, false
	}
	switch probe.Typename {
	case "CheckRun":
		var n struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			DetailsURL string `json:"detailsUrl"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			return CheckContext{}, false
		}
		return CheckContext{
			Name:       n.Name,
			Status:     normalizeRunStatus(n.Status),
			Conclusion: strings.ToLower(n.Conclusion),
			DetailsURL: n.DetailsURL,
		}, true
	case "StatusContext":
		var n struct {
			Context   string `json:"context"`
			State     string `json:"state"`
			TargetURL string `json:"targetUrl"`
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			return CheckContext{}, false
		}
		status, conclusion := normalizeStatusState(n.State)
		return CheckContext{
			Name:            n.Context,
			Status:          status,
			Conclusion:      conclusion,
			DetailsURL:      n.TargetURL,
			IsStatusContext: true,
		}, true
	default:
		return CheckContext{}, false
	}
}

// normalizeRunStatus maps a GraphQL CheckRun.status enum to our lifecycle.
// Only COMPLETED is terminal; QUEUED/IN_PROGRESS/WAITING/PENDING/REQUESTED are
// all still running.
func normalizeRunStatus(s string) string {
	switch strings.ToUpper(s) {
	case "COMPLETED":
		return "completed"
	case "IN_PROGRESS":
		return "in_progress"
	default:
		return "queued"
	}
}

// normalizeStatusState maps a legacy StatusContext.state onto (status,
// conclusion). Empty conclusion means still running.
func normalizeStatusState(s string) (status, conclusion string) {
	switch strings.ToUpper(s) {
	case "SUCCESS":
		return "completed", "success"
	case "FAILURE":
		return "completed", "failure"
	case "ERROR":
		return "completed", "error"
	case "PENDING":
		return "in_progress", ""
	default: // EXPECTED and any unknown state
		return "queued", ""
	}
}
