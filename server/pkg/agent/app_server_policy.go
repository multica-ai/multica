package agent

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
