package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/daemon"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
)

// DispatchOnce attempts a single claim on the given runtime. If a task comes
// back, it provisions the per-issue PVC and creates the worker Job. Returns
// dispatched=true if a Job was created.
func DispatchOnce(ctx context.Context, cli *daemon.Client, k kubernetes.Interface, namespace, imagePullSecret string, r Registered, cb ClaudeBrokerOptions, rc RepoCacheOptions) (bool, error) {
	task, err := cli.ClaimTask(ctx, r.RuntimeID)
	if err != nil {
		return false, fmt.Errorf("claim: %w", err)
	}
	if task == nil || task.ID == "" {
		return false, nil
	}

	pvc, err := EnsurePVC(ctx, k, namespace, r, *task)
	if err != nil {
		return false, fmt.Errorf("ensure pvc: %w", err)
	}

	if _, err := DispatchJob(ctx, k, namespace, r, *task, imagePullSecret, pvc, cb, rc); err != nil {
		// If the Job already exists (controller restart that re-claimed the
		// same task before TTL cleanup), treat as already-dispatched.
		if apierrors.IsAlreadyExists(errors.Unwrap(err)) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}
