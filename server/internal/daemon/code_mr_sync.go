package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	codeMRA1CommandTimeout = 20 * time.Second
	codeMRA1OutputLimit    = 2 << 20
	codeMRA1ErrorLimit     = 1024
	// Headroom on top of the three a1 command budgets so the final snapshot
	// report still has time to land when the commands run slow.
	codeMRSnapshotReportHeadroom = 30 * time.Second
)

var codeMRRepositoryPathRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*)+$`)

type boundedBuffer struct {
	buf       bytes.Buffer
	remaining int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.remaining <= 0 {
		b.truncated = true
		return original, nil
	}
	if len(p) > b.remaining {
		p = p[:b.remaining]
		b.truncated = true
	}
	_, _ = b.buf.Write(p)
	b.remaining -= len(p)
	return original, nil
}

func (b *boundedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *boundedBuffer) String() string { return b.buf.String() }

func validateCodeMRSyncRequest(req protocol.CodeMRSyncPayload) error {
	if strings.TrimSpace(req.RuntimeID) == "" {
		return errors.New("runtime_id is required")
	}
	if _, err := uuid.Parse(req.ExternalPullRequestID); err != nil {
		return errors.New("external_pull_request_id must be a UUID")
	}
	if req.ReviewNumber <= 0 {
		return errors.New("review_number must be positive")
	}
	if !codeMRRepositoryPathRE.MatchString(req.RepositoryPath) {
		return errors.New("repository_path is invalid")
	}
	for _, segment := range strings.Split(req.RepositoryPath, "/") {
		if segment == "." || segment == ".." || strings.HasPrefix(segment, "-") {
			return errors.New("repository_path is invalid")
		}
	}
	return nil
}

func runA1JSON(ctx context.Context, a1Path string, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, codeMRA1CommandTimeout)
	defer cancel()
	stdout := &boundedBuffer{remaining: codeMRA1OutputLimit}
	stderr := &boundedBuffer{remaining: codeMRA1ErrorLimit}
	cmd := exec.CommandContext(cmdCtx, a1Path, args...)
	cmd.Env = append(os.Environ(), "A1_NO_UPDATE_CHECK=1")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if cmdCtx.Err() != nil {
			return nil, fmt.Errorf("a1 timed out: %w", cmdCtx.Err())
		}
		return nil, fmt.Errorf("a1 failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.truncated {
		return nil, errors.New("a1 output exceeded limit")
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}

func parseA1CodeMRSnapshot(viewJSON, statusJSON, commentsJSON []byte) (protocol.CodeMRSnapshotResult, error) {
	var view struct {
		MergeRequest *struct {
			ID           int64  `json:"id"`
			Title        string `json:"title"`
			State        string `json:"state"`
			SourceBranch string `json:"sourceBranch"`
			TargetBranch string `json:"targetBranch"`
			CreatedAt    string `json:"createdAt"`
			UpdatedAt    string `json:"updatedAt"`
			Author       struct {
				Username string `json:"username"`
			} `json:"author"`
		} `json:"mergeRequest"`
	}
	if err := json.Unmarshal(viewJSON, &view); err != nil || view.MergeRequest == nil {
		return protocol.CodeMRSnapshotResult{}, errors.New("invalid a1 mr view response")
	}
	state, err := normalizeA1MRState(view.MergeRequest.State)
	if err != nil {
		return protocol.CodeMRSnapshotResult{}, err
	}
	if strings.TrimSpace(view.MergeRequest.Title) == "" || view.MergeRequest.ID <= 0 {
		return protocol.CodeMRSnapshotResult{}, errors.New("a1 mr view response is incomplete")
	}

	var status struct {
		MRID         int64 `json:"mrId"`
		ReadyToMerge *bool `json:"readyToMerge"`
	}
	if len(bytes.TrimSpace(statusJSON)) > 0 {
		// The mergeability check is optional enrichment, so malformed or
		// mismatched status output is dropped with the same best-effort
		// stance as a failed `mr status` command — the snapshot still lands.
		if err := json.Unmarshal(statusJSON, &status); err != nil {
			status.ReadyToMerge = nil
		} else if status.MRID != 0 && status.MRID != view.MergeRequest.ID {
			status.ReadyToMerge = nil
		}
	}

	var comments []struct {
		Closed int `json:"closed"`
	}
	if err := json.Unmarshal(commentsJSON, &comments); err != nil {
		return protocol.CodeMRSnapshotResult{}, errors.New("invalid a1 mr comments response")
	}
	unresolved := 0
	for _, comment := range comments {
		if comment.Closed == 0 {
			unresolved++
		}
	}
	return protocol.CodeMRSnapshotResult{
		Title:                  view.MergeRequest.Title,
		State:                  state,
		SourceBranch:           view.MergeRequest.SourceBranch,
		TargetBranch:           view.MergeRequest.TargetBranch,
		AuthorLogin:            view.MergeRequest.Author.Username,
		CreatedAt:              view.MergeRequest.CreatedAt,
		UpdatedAt:              view.MergeRequest.UpdatedAt,
		ReadyToMerge:           status.ReadyToMerge,
		CommentCount:           int32(len(comments)),
		UnresolvedCommentCount: int32(unresolved),
	}, nil
}

func normalizeA1MRState(state string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "open", "opened":
		return "open", nil
	case "closed":
		return "closed", nil
	case "merged", "accepted":
		return "merged", nil
	case "draft":
		return "draft", nil
	default:
		return "", fmt.Errorf("unsupported a1 MR state %q", state)
	}
}

func formatA1CommandError(operation, detail string) string {
	detail = strings.TrimSpace(redact.Text(detail))
	detail = util.TruncateUTF8(detail, codeMRA1ErrorLimit)
	if detail == "" {
		return operation + " failed"
	}
	return operation + " failed: " + detail
}

func (d *Daemon) handleCodeMRSync(req protocol.CodeMRSyncPayload) {
	if err := validateCodeMRSyncRequest(req); err != nil {
		d.logger.Debug("code MR sync request dropped: invalid payload", "error", err)
		return
	}
	if d.findRuntime(req.RuntimeID) == nil {
		// The runtime may have re-registered on another daemon (or this frame
		// was relayed to the wrong node). Log it so a stuck sync is traceable.
		d.logger.Debug("code MR sync request dropped: unknown runtime", "runtime_id", req.RuntimeID, "external_pull_request_id", req.ExternalPullRequestID)
		return
	}
	ctx, cancel := context.WithTimeout(d.recoveryContext(), 3*codeMRA1CommandTimeout+codeMRSnapshotReportHeadroom)
	defer cancel()

	result := protocol.CodeMRSnapshotResult{}
	a1Path, err := exec.LookPath("a1")
	if err != nil {
		result.Error = "a1 is not installed on the daemon host"
		d.reportCodeMRSnapshot(ctx, req, result)
		return
	}
	reviewID := strconv.FormatInt(int64(req.ReviewNumber), 10)
	view, err := runA1JSON(ctx, a1Path, "repo", "mr", "view", reviewID, "--repo", req.RepositoryPath, "-f", "json")
	if err != nil {
		result.Error = formatA1CommandError("a1 repo mr view", err.Error())
		d.reportCodeMRSnapshot(ctx, req, result)
		return
	}
	status, statusErr := runA1JSON(ctx, a1Path, "repo", "mr", "status", reviewID, "--repo", req.RepositoryPath, "-f", "json")
	if statusErr != nil {
		status = nil
	}
	comments, err := runA1JSON(ctx, a1Path, "repo", "mr", "comment", "list", "--mr", reviewID, "--repo", req.RepositoryPath, "--sort", "asc", "-f", "json")
	if err != nil {
		result.Error = formatA1CommandError("a1 repo mr comment list", err.Error())
		d.reportCodeMRSnapshot(ctx, req, result)
		return
	}
	result, err = parseA1CodeMRSnapshot(view, status, comments)
	if err != nil {
		result.Error = formatA1CommandError("parse a1 MR snapshot", err.Error())
	}
	d.reportCodeMRSnapshot(ctx, req, result)
}

// reportCodeMRSnapshot sends the snapshot and logs delivery failures so
// production sync breakdowns are diagnosable from every call site.
func (d *Daemon) reportCodeMRSnapshot(ctx context.Context, req protocol.CodeMRSyncPayload, result protocol.CodeMRSnapshotResult) {
	if err := d.client.ReportCodeMRSnapshot(ctx, req.RuntimeID, req.ExternalPullRequestID, result); err != nil {
		d.logger.Warn("code MR snapshot report failed", "runtime_id", req.RuntimeID, "external_pull_request_id", req.ExternalPullRequestID, "error", err)
	}
}
