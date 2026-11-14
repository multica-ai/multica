package daemon

import (
	"log/slog"
	"path/filepath"
)

// layerTaskEnvironment assembles final provider overrides after custom_env.
func layerTaskEnvironment(env, custom map[string]string, hermesHome, provider, mode, root string, logger *slog.Logger) {
	layerCustomEnvAndHermesHome(env, custom, hermesHome, logger)
	// Only opted-in OpenClaw tasks override legacy custom state settings.
	if provider == "openclaw" && mode == "isolated-local" {
		env["OPENCLAW_STATE_DIR"] = filepath.Join(root, "openclaw-state")
		env["OPENCLAW_AGENT_DIR"] = ""
		env["PI_CODING_AGENT_DIR"] = ""
	}
}
