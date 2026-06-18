package main

import (
	"os"
	"strings"
)

type localRunProvider interface {
	Name() string
	Run(args []string, cwd string, env localCLIEnv, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error)
}

var localRunProviders = []localRunProvider{
	codexLocalRunProvider{},
	claudeLocalRunProvider{},
	agyLocalRunProvider{},
}

func localRunProviderForCLI(cliName string) (localRunProvider, bool) {
	normalized := strings.ToLower(strings.TrimSpace(cliName))
	for _, provider := range localRunProviders {
		if provider.Name() == normalized {
			return provider, true
		}
	}
	return nil, false
}

type codexLocalRunProvider struct{}

func (codexLocalRunProvider) Name() string { return "codex" }

func (codexLocalRunProvider) Run(args []string, cwd string, env localCLIEnv, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error) {
	return executeCodexRemoteCLI(args, cwd, env, reporter, usageReporter)
}

type claudeLocalRunProvider struct{}

func (claudeLocalRunProvider) Name() string { return "claude" }

func runProviderPTY(args []string, cwd string, env localCLIEnv, initialStdin string) (int, error) {
	return runLocalRunPTY(localRunPTYOptions{
		Args:         args,
		Cwd:          cwd,
		Env:          localCLIProcessEnv(os.Environ(), env),
		InitialStdin: initialStdin,
	})
}
