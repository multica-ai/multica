package main

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/daemon"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SweepFailedJobs lists controller-managed Jobs in `namespace`, posts FailTask
// for those that ended Failed, then deletes them. Intended to be called
// periodically (~30s) from the main loop.
func SweepFailedJobs(ctx context.Context, cli *daemon.Client, k kubernetes.Interface, namespace string) error {
	jobs, err := k.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelManagedBy + "=" + managedByValue,
	})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	for _, j := range jobs.Items {
		if !jobIsFailed(&j) {
			continue
		}
		taskID := j.Labels[labelTaskID]
		if taskID == "" {
			continue
		}
		reason := jobFailureReason(&j)
		_ = cli.FailTask(ctx, taskID, reason, "", "", "agent_error")
		_ = k.BatchV1().Jobs(namespace).Delete(ctx, j.Name, metav1.DeleteOptions{})
	}
	return nil
}

func jobIsFailed(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == "True" {
			return true
		}
	}
	return j.Status.Failed > 0
}

func jobFailureReason(j *batchv1.Job) string {
	for _, c := range j.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == "True" {
			if c.Reason != "" || c.Message != "" {
				return fmt.Sprintf("Job failed: %s — %s", c.Reason, c.Message)
			}
		}
	}
	return "Job failed (no condition message)"
}
