package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// expectedRevisionFlag is the name of the optimistic-concurrency precondition
// flag shared by the mutating issue commands.
const expectedRevisionFlag = "expected-revision"

// readExpectedRevision validates the optional precondition before any request.
// Zero means the flag was omitted and preserves unconditional writes.
func readExpectedRevision(cmd *cobra.Command) (int64, error) {
	if !cmd.Flags().Changed(expectedRevisionFlag) {
		return 0, nil
	}
	rev, err := cmd.Flags().GetInt64(expectedRevisionFlag)
	if err != nil {
		return 0, err
	}
	if rev < 1 {
		return 0, fmt.Errorf("--%s must be a positive integer", expectedRevisionFlag)
	}
	return rev, nil
}

func issueMutationError(cmd *cobra.Command, action string, err error) error {
	output, _ := cmd.Flags().GetString("output")
	var httpErr *cli.HTTPError
	if cmd.Flags().Changed(expectedRevisionFlag) && output == "json" && errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
		var conflict struct {
			Code             string `json:"code"`
			Error            string `json:"error"`
			ResourceType     string `json:"resource_type"`
			ResourceID       string `json:"resource_id,omitempty"`
			ExpectedRevision int64  `json:"expected_revision"`
			ActualRevision   int64  `json:"actual_revision"`
		}
		if json.Unmarshal([]byte(httpErr.Body), &conflict) == nil && conflict.Code == "revision_conflict" && conflict.ResourceType == "issue" && conflict.ExpectedRevision > 0 && conflict.ActualRevision > 0 {
			conflict.Error = "issue changed; re-read and reconcile before retrying"
			if printErr := cli.PrintJSON(os.Stdout, conflict); printErr != nil {
				return printErr
			}
			return errSilent
		}
	}
	return issueDescriptionWriteError(action, err)
}

func registerIssueExpectedRevisionFlag(cmd *cobra.Command) {
	cmd.Flags().Int64(expectedRevisionFlag, 0, "Require this positive issue revision; stale writes fail without changes")
}

func issueMutationPath(issueID, suffix string, expected int64) string {
	path := "/api/issues/" + url.PathEscape(issueID)
	if expected > 0 {
		// Older servers must reject the route rather than ignore a new query value.
		path += "/conditional"
	}
	path += suffix
	if expected > 0 {
		path += "?expected_revision=" + strconv.FormatInt(expected, 10)
	}
	return path
}
