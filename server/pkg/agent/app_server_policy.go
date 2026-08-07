package agent

import "strings"

type appServerPolicy struct {
	provider                  string
	defaultExecutable         string
	manageCodexConfig         bool
	allowCodexLaunchOverrides bool
	allowCodexTurnOverrides   bool
	allowCodexStartupRetry    bool
}

var codexAppServerPolicy = appServerPolicy{
	provider:                  "codex",
	defaultExecutable:         "codex",
	manageCodexConfig:         true,
	allowCodexLaunchOverrides: true,
	allowCodexTurnOverrides:   true,
	allowCodexStartupRetry:    true,
}

var platformAgentAppServerPolicy = appServerPolicy{
	provider:          "platform-agent-cli",
	defaultExecutable: "platform-agent-cli",
}

func (p appServerPolicy) execOptions(opts ExecOptions) ExecOptions {
	if !p.allowCodexLaunchOverrides {
		opts.ExtraArgs = nil
		opts.CustomArgs = nil
		opts.McpConfig = nil
	}
	if !p.allowCodexTurnOverrides {
		opts.Model = ""
		opts.ThinkingLevel = ""
		opts.ServiceTier = ""
	}
	return opts
}

func (p appServerPolicy) childEnv(extra map[string]string) []string {
	env := buildEnv(extra)
	if p.manageCodexConfig {
		return env
	}
	return withoutEnvKeyFold(env, "CODEX_HOME")
}

func withoutEnvKeyFold(env []string, excludedKey string) []string {
	filtered := env[:0]
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, excludedKey) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func appServerRetryReason(policy appServerPolicy, result Result) string {
	if !policy.allowCodexStartupRetry {
		return ""
	}
	if result.codexInitializeRetrySafe {
		return "initialize"
	}
	if result.codexStartupRefreshRetrySafe {
		return "model_catalog_refresh"
	}
	return ""
}
