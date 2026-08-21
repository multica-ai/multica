package daemon

import "testing"

func TestLayerTaskTokensInjects(t *testing.T) {
	agentEnv := map[string]string{"CODEX_HOME": "/tmp/codex"}
	layerTaskTokens(agentEnv, map[string]string{"BOT_TOKEN_ERP": "jwt-erp"}, nil)

	if got := agentEnv["BOT_TOKEN_ERP"]; got != "jwt-erp" {
		t.Errorf("agentEnv[BOT_TOKEN_ERP] = %q, want jwt-erp", got)
	}
	if got := agentEnv["CODEX_HOME"]; got != "/tmp/codex" {
		t.Errorf("agentEnv[CODEX_HOME] = %q, want it untouched", got)
	}
}

func TestLayerTaskTokensRespectsBlocklist(t *testing.T) {
	agentEnv := map[string]string{"PATH": "/usr/bin"}
	// A misconfigured catalog must not be able to hijack daemon-owned
	// variables even if the server-side name validation is ever relaxed.
	layerTaskTokens(agentEnv, map[string]string{
		"PATH":        "/evil",
		"MULTICA_FOO": "x",
		"BOT_TOKEN_A": "ok",
	}, nil)

	if got := agentEnv["PATH"]; got != "/usr/bin" {
		t.Errorf("agentEnv[PATH] = %q, want the daemon value preserved", got)
	}
	if _, present := agentEnv["MULTICA_FOO"]; present {
		t.Error("agentEnv[MULTICA_FOO] was set, want it blocked")
	}
	if got := agentEnv["BOT_TOKEN_A"]; got != "ok" {
		t.Errorf("agentEnv[BOT_TOKEN_A] = %q, want ok", got)
	}
}

func TestLayerTaskTokensNoopWhenEmpty(t *testing.T) {
	agentEnv := map[string]string{"CODEX_HOME": "/tmp/codex"}
	layerTaskTokens(agentEnv, nil, nil)

	if len(agentEnv) != 1 {
		t.Errorf("agentEnv = %v, want unchanged", agentEnv)
	}
}

// Custom env must still win: it is the documented local-debugging override,
// and layering order is what guarantees it.
func TestCustomEnvOverridesTaskToken(t *testing.T) {
	agentEnv := map[string]string{}
	layerTaskTokens(agentEnv, map[string]string{"BOT_TOKEN_ERP": "from-server"}, nil)
	layerCustomEnvAndHermesHome(agentEnv, map[string]string{"BOT_TOKEN_ERP": "from-custom-env"}, "", nil)

	if got := agentEnv["BOT_TOKEN_ERP"]; got != "from-custom-env" {
		t.Errorf("agentEnv[BOT_TOKEN_ERP] = %q, want custom_env to win", got)
	}
}
