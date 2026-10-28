package ghsnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// CheckContext is one normalized check for a PR head. Both check runs and commit
// statuses are flattened into this shape, so
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
// API returned — no incremental inference from webhook events.
type PRSnapshot struct {
	HeadSHA          string
	Mergeable        string // MERGEABLE / CONFLICTING / UNKNOWN (raw enum)
	MergeStateStatus string // CLEAN / DIRTY / BLOCKED / BEHIND / UNSTABLE / ... (raw enum)
	// RollupState aggregates the current head's check runs and commit statuses.
	// Empty ONLY when HasChecks is false.
	RollupState string
	// HasChecks is false when both API collections are empty. This must NEVER be rendered
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

const (
	maxSnapshotContextPages = 100
	snapshotPageSize        = 100
)

type restPullRequest struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"`
}

type restCheckRun struct {
	ID         int64  `json:"id"`
	HeadSHA    string `json:"head_sha"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"details_url"`
}

type restStatusContext struct {
	Context   string `json:"context"`
	State     string `json:"state"`
	TargetURL string `json:"target_url"`
}

// FetchPRSnapshot uses only Pull requests, Checks and Commit statuses read
// permissions. Every page is pinned to one head; a final PR read detects pushes
// before the guarded atomic write in refresh.go.
func FetchPRSnapshot(ctx context.Context, c *Client, installationID int64, owner, repo string, number int32) (*PRSnapshot, error) {
	if !c.Enabled() {
		return nil, errors.New("ghsnapshot: client not configured")
	}
	base := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	prPath := fmt.Sprintf("%s/pulls/%d", base, number)
	pr, err := fetchSnapshotPR(ctx, c, installationID, prPath)
	if err != nil {
		return nil, err
	}
	commitPath := base + "/commits/" + url.PathEscape(pr.Head.SHA)
	runs, err := fetchSnapshotPages[restCheckRun](ctx, c, installationID, commitPath+"/check-runs?filter=latest", "check_runs")
	if err != nil {
		return nil, err
	}
	statuses, err := fetchSnapshotPages[restStatusContext](ctx, c, installationID, commitPath+"/status", "statuses")
	if err != nil {
		return nil, err
	}

	snap := &PRSnapshot{HeadSHA: pr.Head.SHA}
	seenRuns := make(map[int64]struct{}, len(runs))
	for _, run := range runs {
		if run.ID <= 0 || run.Name == "" || run.HeadSHA != snap.HeadSHA {
			return nil, errors.New("ghsnapshot: invalid check run or mismatched head")
		}
		if _, seen := seenRuns[run.ID]; seen {
			return nil, errors.New("ghsnapshot: duplicate check run during pagination")
		}
		seenRuns[run.ID] = struct{}{}
		status := normalizeRunStatus(run.Status)
		conclusion := strings.ToLower(run.Conclusion)
		if status != "completed" {
			conclusion = ""
		} else if rollupConclusion(conclusion) == "PENDING" {
			// Unknown/empty conclusions must remain undecided in both the
			// refresh queue and the persisted running-check counts.
			status, conclusion = "queued", ""
		}
		snap.Contexts = append(snap.Contexts, CheckContext{Name: run.Name, Status: status, Conclusion: conclusion, DetailsURL: run.DetailsURL})
	}
	seenStatuses := make(map[string]struct{}, len(statuses))
	for _, entry := range statuses {
		if entry.Context == "" {
			return nil, errors.New("ghsnapshot: missing status context")
		}
		if _, seen := seenStatuses[entry.Context]; seen {
			return nil, errors.New("ghsnapshot: duplicate status context during pagination")
		}
		seenStatuses[entry.Context] = struct{}{}
		status, conclusion := normalizeStatusState(entry.State)
		snap.Contexts = append(snap.Contexts, CheckContext{Name: entry.Context, Status: status, Conclusion: conclusion, DetailsURL: entry.TargetURL, IsStatusContext: true})
	}

	latest, err := fetchSnapshotPR(ctx, c, installationID, prPath)
	if err != nil {
		return nil, err
	}
	if latest.Head.SHA != snap.HeadSHA {
		return nil, errors.New("ghsnapshot: pull request head changed during fetch")
	}
	snap.Mergeable = "UNKNOWN"
	if latest.Mergeable != nil {
		if *latest.Mergeable {
			snap.Mergeable = "MERGEABLE"
		} else {
			snap.Mergeable = "CONFLICTING"
		}
	}
	snap.MergeStateStatus = strings.ToUpper(latest.MergeableState)
	if latest.Mergeable == nil || snap.MergeStateStatus == "" {
		snap.MergeStateStatus = "UNKNOWN"
	}
	snap.HasChecks = len(snap.Contexts) > 0
	if snap.HasChecks {
		snap.RollupState = rollupContexts(snap.Contexts)
	}
	return snap, nil
}

func fetchSnapshotPR(ctx context.Context, c *Client, installationID int64, path string) (*restPullRequest, error) {
	data, err := c.restGet(ctx, installationID, path)
	if err != nil {
		return nil, err
	}
	var pr restPullRequest
	if json.Unmarshal(data, &pr) != nil || pr.Head.SHA == "" {
		return nil, errors.New("ghsnapshot: malformed pull request data")
	}
	return &pr, nil
}

// Counts must stay consistent across pages. Never commit a truncated collection
// or follow a pagination URL carrying the installation token to another host.
func fetchSnapshotPages[T any](ctx context.Context, c *Client, installationID int64, path, collection string) ([]T, error) {
	var items []T
	expected := -1
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	for page := 1; page <= maxSnapshotContextPages; page++ {
		data, err := c.restGet(ctx, installationID, fmt.Sprintf("%s%sper_page=%d&page=%d", path, separator, snapshotPageSize, page))
		if err != nil {
			return nil, err
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(data, &envelope) != nil {
			return nil, errors.New("ghsnapshot: malformed check page")
		}
		var count *int
		var entries []T
		if json.Unmarshal(envelope["total_count"], &count) != nil || count == nil || *count < 0 ||
			json.Unmarshal(envelope[collection], &entries) != nil || entries == nil || len(entries) > snapshotPageSize {
			return nil, errors.New("ghsnapshot: malformed check collection")
		}
		if expected == -1 {
			expected = *count
		} else if *count != expected {
			return nil, errors.New("ghsnapshot: check count changed during pagination")
		}
		items = append(items, entries...)
		if len(items) > expected {
			return nil, errors.New("ghsnapshot: check collection exceeds reported count")
		}
		if len(items) == expected {
			return items, nil
		}
		if len(entries) == 0 {
			return nil, errors.New("ghsnapshot: truncated check collection")
		}
	}
	return nil, errors.New("ghsnapshot: check-context pagination exceeds page limit")
}

func rollupContexts(contexts []CheckContext) string {
	pending := false
	for _, cc := range contexts {
		if cc.Status != "completed" {
			pending = true
			continue
		}
		switch rollupConclusion(cc.Conclusion) {
		case "FAILURE":
			return "FAILURE"
		case "PENDING":
			pending = true
		}
	}
	if pending {
		return "PENDING"
	}
	return "SUCCESS"
}

func rollupConclusion(conclusion string) string {
	switch conclusion {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale", "error":
		return "FAILURE"
	case "success", "neutral", "skipped":
		return "SUCCESS"
	default:
		return "PENDING"
	}
}

// normalizeRunStatus maps a check run status to our lifecycle.
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
