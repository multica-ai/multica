package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/daemon"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	labelTaskID    = "multica.ai/task-id"
	labelWsID      = "multica.ai/workspace-id"
	labelAgentID   = "multica.ai/agent-id"
	labelIssueID   = "multica.ai/issue-id"
	labelRuntimeID = "multica.ai/runtime-id"
	labelManagedBy = "app.kubernetes.io/managed-by"
	managedByValue = "multica-k8s-controller"
)

func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

// pvcName is deterministic so a follow-up task on the same (ws, agent, scope)
// reuses the same PVC. Scope = issue when present; otherwise chat session,
// autopilot run, or the task id as a last resort (per-task workdir).
func pvcName(r Registered, t daemon.Task) string {
	scope := shortID(t.IssueID)
	switch {
	case t.IssueID != "":
		scope = shortID(t.IssueID)
	case t.ChatSessionID != "":
		scope = "c" + shortID(t.ChatSessionID)
	case t.AutopilotRunID != "":
		scope = "a" + shortID(t.AutopilotRunID)
	default:
		scope = "t" + shortID(t.ID)
	}
	return fmt.Sprintf("wd-%s-%s-%s",
		shortID(r.WorkspaceID), shortID(t.AgentID), scope)
}

// EnsurePVC creates a per-issue workdir PVC if missing, returns its name.
func EnsurePVC(ctx context.Context, k kubernetes.Interface, namespace string, r Registered, t daemon.Task) (string, error) {
	name := pvcName(r, t)
	_, err := k.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return name, nil
	}
	if !errors.IsNotFound(err) {
		return "", fmt.Errorf("lookup pvc %s: %w", name, err)
	}

	q, err := resource.ParseQuantity(r.PVCSize)
	if err != nil {
		return "", fmt.Errorf("parse pvcSize %q: %w", r.PVCSize, err)
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelWsID:      r.WorkspaceID,
				labelAgentID:   t.AgentID,
				labelIssueID:   t.IssueID,
				labelManagedBy: managedByValue,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: q},
			},
		},
	}
	if r.StorageClass != "" {
		sc := r.StorageClass
		pvc.Spec.StorageClassName = &sc
	}
	if _, err := k.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("create pvc %s: %w", name, err)
	}
	return name, nil
}

// DispatchJob writes the Task payload to a ConfigMap and creates the worker Job
// that mounts the payload + workdir PVC + the three worker Secrets, and runs
// `multica run-task`. Returns the Job name.
func DispatchJob(ctx context.Context, k kubernetes.Interface, namespace string, r Registered, t daemon.Task, imagePullSecret, pvc string) (string, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return "", fmt.Errorf("marshal task: %w", err)
	}
	cmName := "task-" + t.ID
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: cmName, Namespace: namespace,
			Labels: jobLabels(r, t),
		},
		Data: map[string]string{"task.json": string(payload)},
	}
	if _, err := k.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
		return "", fmt.Errorf("create payload CM: %w", err)
	}

	jobName := "task-" + shortID(t.ID)
	ttl := int32(3600)
	nonRoot := true
	uid := int64(1001)
	gid := int64(1001)
	mode := int32(0o400)
	allowPrivEsc := false
	seccompRuntimeDefault := corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	containerSC := &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivEsc,
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		SeccompProfile:           &seccompRuntimeDefault,
		RunAsNonRoot:             &nonRoot,
	}
	postStart := []string{
		"sh", "-c",
		"cp /home/multica/.ssh-src/id_ed25519 /home/multica/.ssh/id_ed25519 2>/dev/null; chmod 600 /home/multica/.ssh/id_ed25519 2>/dev/null; true",
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName, Namespace: namespace, Labels: jobLabels(r, t),
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: jobLabels(r, t)},
				Spec: corev1.PodSpec{
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: imagePullSecret}},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   &nonRoot,
						RunAsUser:      &uid,
						RunAsGroup:     &gid,
						FSGroup:        &gid,
						SeccompProfile: &seccompRuntimeDefault,
					},
					InitContainers: []corev1.Container{{
						Name: "claude-auth", Image: r.Image,
						Command: []string{"sh", "-c", "tar xzf /secret/claude-auth.tgz -C /home/multica/.claude --strip-components=1"},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "claude-oauth-secret", MountPath: "/secret", ReadOnly: true},
							{Name: "claude-home", MountPath: "/home/multica/.claude"},
						},
						SecurityContext: containerSC,
					}},
					Containers: []corev1.Container{{
						Name:    "runtask",
						Image:   r.Image,
						Command: []string{"multica", "run-task", "--task-file", "/etc/task/task.json", "--workspaces-root", "/work"},
						Env: []corev1.EnvVar{
							{Name: "MULTICA_SERVER_URL", Value: "http://multica-backend." + namespace + ".svc:8080"},
							{Name: "MULTICA_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "multica-token"}, Key: "token"}}},
							{Name: "HOME", Value: "/home/multica"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "payload", MountPath: "/etc/task"},
							{Name: "claude-home", MountPath: "/home/multica/.claude"},
							{Name: "git-ssh", MountPath: "/home/multica/.ssh-src", ReadOnly: true},
							{Name: "work", MountPath: "/work"},
						},
						Lifecycle:       &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: postStart}}},
						SecurityContext: containerSC,
					}},
					Volumes: []corev1.Volume{
						{Name: "payload", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cmName}}}},
						{Name: "claude-oauth-secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "multica-claude-oauth"}}},
						{Name: "claude-home", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "git-ssh", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "multica-git-ssh", DefaultMode: &mode}}},
						{Name: "work", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc}}},
					},
				},
			},
		},
	}
	if _, err := k.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}
	return jobName, nil
}

func jobLabels(r Registered, t daemon.Task) map[string]string {
	return map[string]string{
		labelManagedBy: managedByValue,
		labelTaskID:    t.ID,
		labelWsID:      r.WorkspaceID,
		labelAgentID:   t.AgentID,
		labelIssueID:   t.IssueID,
		labelRuntimeID: r.RuntimeID,
	}
}
