package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

// Per-agent kubeconfig auto-injection.
//
// The runtime StatefulSet on lue-kube projects one kubeconfig per agent
// identity into a shared volume — e.g. /etc/multica/kube/boris.kubeconfig
// for the Bermont CTO, /etc/multica/kube/doug.kubeconfig for the CCS CTO,
// etc. This file maps a dispatched agent's display name onto the matching
// path so the daemon can set KUBECONFIG automatically per task — the
// agent's CLI then runs as a distinct ServiceAccount, with audit logs in
// the target cluster attributing API calls per-agent rather than to a
// shared service-account.
//
// Resolution is best-effort: if no matching file exists, KUBECONFIG is
// left unset and the agent runs without cluster credentials (or with
// whatever its CustomEnv supplies). This keeps the daemon usable in
// development environments where the kubeconfig volume isn't mounted.

// kubeconfigDirEnv lets operators override the per-agent kubeconfig
// directory (handy for tests and non-cluster deployments).
const kubeconfigDirEnv = "MULTICA_KUBECONFIG_DIR"

// defaultKubeconfigDir matches the projected-volume mount in
// k3s/apps/multica/base/runtime.yaml on lue-kube.
const defaultKubeconfigDir = "/etc/multica/kube"

// kubeconfigPathForAgent returns the absolute path to the per-agent
// kubeconfig if a matching file exists, or "" if none. The slug rule
// is: take the first whitespace-delimited token of the agent's display
// name, lowercase it, and keep [a-z0-9-]. So "Boris" → "boris",
// "Doug - CTO" → "doug", "Kael OpenClaw" → "kael".
func kubeconfigPathForAgent(name string) string {
	slug := slugifyAgentName(name)
	if slug == "" {
		return ""
	}
	dir := os.Getenv(kubeconfigDirEnv)
	if dir == "" {
		dir = defaultKubeconfigDir
	}
	path := filepath.Join(dir, slug+".kubeconfig")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

// slugifyAgentName extracts a stable filesystem slug from a display name.
// Empty input or names with no alphanumeric characters return "".
func slugifyAgentName(name string) string {
	tokens := strings.Fields(strings.TrimSpace(name))
	if len(tokens) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToLower(tokens[0]) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}
