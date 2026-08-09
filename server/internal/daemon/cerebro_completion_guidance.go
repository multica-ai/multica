package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (c *Client) CompleteTaskWithGuidance(ctx context.Context, taskID, output, branchName, sessionID, workDir string, completionAttempt int) (*CompletionGuidance, error) {
	body := map[string]any{"output": output}
	if completionAttempt > 1 {
		body["completion_attempt"] = completionAttempt
	}
	if branchName != "" {
		body["branch_name"] = branchName
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	err := c.postJSONWithRetry(ctx, fmt.Sprintf("/api/daemon/tasks/%s/complete", taskID), body, nil, defaultTerminalRetrySchedule)
	if err == nil {
		return nil, nil
	}
	var reqErr *requestError
	if !errors.As(err, &reqErr) || reqErr.StatusCode != http.StatusConflict {
		return nil, err
	}
	var guidance CompletionGuidance
	if json.Unmarshal([]byte(reqErr.Body), &guidance) != nil || guidance.Code != "workflow_gate_rejected" || guidance.Requirement == "" {
		return nil, err
	}
	return &guidance, nil
}

func (d *Daemon) startTaskUnlessGuided(ctx context.Context, task Task) error {
	if task.CompletionGuidance != nil {
		return nil
	}
	return d.client.StartTask(ctx, task.ID)
}

func (d *Daemon) guideTaskCompletionOnce(runCtx, reportCtx context.Context, task Task, provider string, slot int, original TaskResult, guidance *CompletionGuidance, taskLog *slog.Logger) TaskResult {
	guidedTask := task
	guidedTask.PriorSessionID = original.SessionID
	guidedTask.PriorWorkDir = original.WorkDir
	guidedTask.CompletionGuidance = guidance
	guidedTask.CompletionOriginalAnswer = original.Comment

	guided, err := d.runner.run(runCtx, guidedTask, provider, slot, taskLog)
	usageEvents := modelUsageEventsForReport(guided, time.Now().UTC())
	if len(guided.Usage) > 0 || len(usageEvents) > 0 {
		if usageErr := d.client.ReportTaskUsage(reportCtx, task.ID, guided.Usage, usageEvents); usageErr != nil {
			taskLog.Warn("report guided task usage failed", "error", usageErr)
		}
	}
	d.reportTaskSkillUsage(reportCtx, task, provider, guided)
	d.enqueueTraceUpload(task, provider, guided)

	if err != nil || guided.Status != "completed" {
		taskLog.Warn("Workflow hook guidance turn did not complete; preserving the original answer", "status", guided.Status, "error", err)
		guided = original
	} else {
		if guided.Status == "" {
			guided.Status = original.Status
		}
		if guided.Comment == "" {
			guided.Comment = original.Comment
		}
		if guided.BranchName == "" {
			guided.BranchName = original.BranchName
		}
		if guided.SessionID == "" {
			guided.SessionID = original.SessionID
		}
		if guided.WorkDir == "" {
			guided.WorkDir = original.WorkDir
		}
		if guided.EnvRoot == "" {
			guided.EnvRoot = original.EnvRoot
		}
	}
	guided.CompletionAttempt = 2

	d.maybeReportAccountUsage(reportCtx, task.RuntimeID, guided.Comment, guided.Logs, guided.Usage, taskLog)
	if repeated := d.reportTaskResult(reportCtx, task.ID, guided, taskLog); repeated != nil {
		taskLog.Error("Workflow hook returned guidance more than once; leaving task running", "requirement", repeated.Requirement)
	}
	return guided
}

func buildCompletionGuidancePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("The Workflow hook rejected your first attempt to stop. The task is still running, so satisfy one of the allowed continuations now.\n\n")
	fmt.Fprintf(&b, "Requirement: %s\n\n", task.CompletionGuidance.Requirement)
	b.WriteString("Allowed alternatives:\n")
	for _, alternative := range task.CompletionGuidance.Alternatives {
		fmt.Fprintf(&b, "- %s\n", alternative)
	}
	b.WriteString("\nIf wakeups are unavailable, use another allowed alternative. Do not merely promise future work.\n\n")
	b.WriteString("Your previous final answer was:\n\n")
	fmt.Fprintf(&b, "<previous_final_answer>\n%s\n</previous_final_answer>\n\n", task.CompletionOriginalAnswer)
	b.WriteString("After creating the continuation: Return a complete final answer for the user. The platform will preserve the previous answer if this corrective turn cannot produce one.\n")
	return b.String()
}
