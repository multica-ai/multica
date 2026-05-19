package main

import (
	"os"
	"strings"
)

type localRunProvider interface {
	Name() string
	Run(args []string, cwd string, env localCLIEnv, initialPrompt string, reporter *localRunReporter) (int, error)
}

var localRunProviders = []localRunProvider{
	codexLocalRunProvider{},
	claudeLocalRunProvider{},
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

func (codexLocalRunProvider) Run(args []string, cwd string, env localCLIEnv, initialPrompt string, reporter *localRunReporter) (int, error) {
	return executeCodexRemoteCLI(args, cwd, env, initialPrompt, reporter)
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
