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
// that mounts the payload + workdir PVC + the worker Secrets, and runs
// `multica run-task`. Returns the Job name.
//
// When cb.Enabled is true, the Job is wired to use the in-cluster
// multica-claude-broker via an apiKeyHelper script (mounted from the broker's
// client ConfigMap) instead of the legacy claude-auth init container that
// expanded a refresh_token tarball into ~/.claude/. The two modes are
// mutually exclusive — when broker mode is on, claude-oauth-secret is not
// mounted at all (worker pods never see the refresh_token).
//
// When rc.Enabled is true (Plan F.1), the Job additionally mounts the cluster
// repo-cache PVC read-only at rc.MountPath and a per-task gitconfig ConfigMap
// at /home/multica/.gitconfig whose url.<base>.insteadOf rewrites turn the
// agent's plain `git clone <origin-url>` into a sub-second local file:// clone.
func DispatchJob(ctx context.Context, k kubernetes.Interface, namespace string, r Registered, t daemon.Task, imagePullSecret, pvc string, cb ClaudeBrokerOptions, rc RepoCacheOptions) (string, error) {
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

	// Per-task gitconfig ConfigMap for the repo-cache URL rewrites. We create
	// it unconditionally when repocache is enabled, even when t.Repos is
	// empty — that's a harmless empty file, and never having to special-case
	// "missing volume" simplifies the pod spec below.
	gitconfigCMName := "task-" + shortID(t.ID) + "-gitconfig"
	if rc.Enabled {
		gitconfigCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: gitconfigCMName, Namespace: namespace,
				Labels: jobLabels(r, t),
			},
			Data: map[string]string{".gitconfig": gitconfigForTask(r.WorkspaceID, rc.MountPath, t.Repos)},
		}
		if _, err := k.CoreV1().ConfigMaps(namespace).Create(ctx, gitconfigCM, metav1.CreateOptions{}); err != nil && !errors.IsAlreadyExists(err) {
			return "", fmt.Errorf("create gitconfig CM: %w", err)
		}
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

	// Volumes always present.
	volumes := []corev1.Volume{
		{Name: "payload", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: cmName}}}},
		{Name: "claude-home", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "git-ssh", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "multica-git-ssh", DefaultMode: &mode}}},
		{Name: "work", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc}}},
	}

	// runtask container template — claude-home + base mounts always present;
	// broker mode adds CLAUDE_CODE_API_KEY_HELPER_TTL_MS env + helper script
	// mount + a settings.json subpath into ~/.claude.
	runtaskEnv := []corev1.EnvVar{
		{Name: "MULTICA_SERVER_URL", Value: "http://multica-backend." + namespace + ".svc:8080"},
		{Name: "MULTICA_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "multica-token"}, Key: "token"}}},
		{Name: "HOME", Value: "/home/multica"},
	}
	runtaskMounts := []corev1.VolumeMount{
		{Name: "payload", MountPath: "/etc/task"},
		{Name: "claude-home", MountPath: "/home/multica/.claude"},
		{Name: "git-ssh", MountPath: "/home/multica/.ssh-src", ReadOnly: true},
		{Name: "work", MountPath: "/work"},
	}

	// initContainers default to the legacy claude-auth path; broker mode
	// drops them entirely (the broker owns auth, worker pods have no
	// refresh_token to expand).
	var initContainers []corev1.Container

	if rc.Enabled {
		volumes = append(volumes,
			corev1.Volume{
				Name: "repocache",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: rc.PVCName,
						ReadOnly:  true,
					},
				},
			},
			corev1.Volume{
				Name: "gitconfig",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: gitconfigCMName},
					},
				},
			},
		)
		runtaskMounts = append(runtaskMounts,
			corev1.VolumeMount{Name: "repocache", MountPath: rc.MountPath, ReadOnly: true},
			corev1.VolumeMount{Name: "gitconfig", MountPath: "/home/multica/.gitconfig", SubPath: ".gitconfig", ReadOnly: true},
		)
	}

	if cb.Enabled {
		// Broker mode: inject CLAUDE_CODE_OAUTH_TOKEN from the broker's
		// access-token Secret. claude treats this env var as a static OAuth
		// bearer (sends as Authorization: Bearer), bypassing both the
		// apiKeyHelper (which is x-api-key path) and the refresh logic
		// (which would re-introduce the rotation race). Access tokens are
		// good for hours — well beyond worker Job lifetime — and the broker
		// keeps the Secret freshened by re-writing on every refresh.
		runtaskEnv = append(runtaskEnv, corev1.EnvVar{
			Name: "CLAUDE_CODE_OAUTH_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: cb.AccessTokenSecret},
					Key:                  cb.SecretKey,
				},
			},
		})
	} else {
		// Legacy mode: mount the OAuth tarball Secret and expand it via the
		// claude-auth init container into ~/.claude/.
		volumes = append(volumes,
			corev1.Volume{Name: "claude-oauth-secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "multica-claude-oauth"}}},
		)
		initContainers = []corev1.Container{{
			Name: "claude-auth", Image: r.Image,
			Command: []string{"sh", "-c", "tar xzf /secret/claude-auth.tgz -C /home/multica/.claude --strip-components=1"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "claude-oauth-secret", MountPath: "/secret", ReadOnly: true},
				{Name: "claude-home", MountPath: "/home/multica/.claude"},
			},
			SecurityContext: containerSC,
		}}
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
					InitContainers: initContainers,
					Containers: []corev1.Container{{
						Name:            "runtask",
						Image:           r.Image,
						Command:         []string{"multica", "run-task", "--task-file", "/etc/task/task.json", "--workspaces-root", "/work"},
						Env:             runtaskEnv,
						VolumeMounts:    runtaskMounts,
						Lifecycle:       &corev1.Lifecycle{PostStart: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: postStart}}},
						SecurityContext: containerSC,
					}},
					Volumes: volumes,
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
