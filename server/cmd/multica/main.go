package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// debugFlag is bound to the persistent --debug flag and, when set, makes
// FormatError emit the full original error chain instead of just the
// user-facing message.
var debugFlag bool

var rootCmd = &cobra.Command{
	Use:               "multica",
	Short:             "Multica CLI — local agent runtime and management tool",
	Long:              "Work seamlessly with Multica from the command line.",
	SilenceUsage:      true,
	SilenceErrors:     true,
	PersistentPreRunE: enforceTaskScopedCLI,
}

var taskScopedCLICommands = map[string]struct{}{
	"multica issue get":          {},
	"multica issue comment list": {},
	"multica issue comment add":  {},
	"multica issue status":       {},
	"multica issue run-messages": {},
	"multica repo checkout":      {},
}

func enforceTaskScopedCLI(cmd *cobra.Command, _ []string) error {
	if !inTaskScopedCLIContext() {
		return nil
	}

	for _, flagName := range []string{"profile", "server-url", "workspace-id"} {
		flag := cmd.Root().PersistentFlags().Lookup(flagName)
		if flag != nil && flag.Changed {
			return fmt.Errorf("task-scoped CLI context prohibits --%s overrides", flagName)
		}
	}

	if _, allowed := taskScopedCLICommands[cmd.CommandPath()]; !allowed {
		return fmt.Errorf("task-scoped CLI context permits only bounded issue commands; %q is prohibited", cmd.CommandPath())
	}
	return nil
}

func inTaskScopedCLIContext() bool {
	return strings.HasPrefix(os.Getenv("MULTICA_TOKEN"), "mat_") || inDaemonManagedExecutionContext()
}

func init() {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)\ngo: %s, os/arch: %s/%s", version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	rootCmd.SetVersionTemplate("multica {{.Version}}\n")

	// Tag every CLI HTTP request with this binary's build version so the
	// server can split logs/metrics by client version.
	cli.ClientVersion = version

	rootCmd.PersistentFlags().String("server-url", "", "Multica server URL (env: MULTICA_SERVER_URL)")
	rootCmd.PersistentFlags().String("workspace-id", "", "Workspace ID (env: MULTICA_WORKSPACE_ID)")
	rootCmd.PersistentFlags().String("profile", "", "Configuration profile name (e.g. dev) — isolates config, daemon state, and workspaces")
	rootCmd.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print full error details on failure (env: MULTICA_DEBUG)")

	// Core commands
	issueCmd.GroupID = groupCore
	projectCmd.GroupID = groupCore
	labelCmd.GroupID = groupCore
	propertyCmd.GroupID = groupCore
	agentCmd.GroupID = groupCore
	autopilotCmd.GroupID = groupCore
	workspaceCmd.GroupID = groupCore
	repoCmd.GroupID = groupCore
	skillCmd.GroupID = groupCore
	squadCmd.GroupID = groupCore
	chatCmd.GroupID = groupCore

	// Runtime commands
	daemonCmd.GroupID = groupRuntime
	runtimeCmd.GroupID = groupRuntime

	// Additional commands
	authCmd.GroupID = groupAdditional
	userCmd.GroupID = groupAdditional
	loginCmd.GroupID = groupAdditional
	setupCmd.GroupID = groupAdditional
	attachmentCmd.GroupID = groupAdditional
	configCmd.GroupID = groupAdditional
	updateCmd.GroupID = groupAdditional
	versionCmd.GroupID = groupAdditional

	rootCmd.AddCommand(issueCmd)
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(labelCmd)
	rootCmd.AddCommand(propertyCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(autopilotCmd)
	rootCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(repoCmd)
	rootCmd.AddCommand(skillCmd)
	rootCmd.AddCommand(squadCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(runtimeCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(attachmentCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(versionCmd)

	initHelp(rootCmd)
}

func main() {
	cli.CleanupStaleUpdateArtifacts()
	if err := rootCmd.Execute(); err != nil {
		if err != errSilent {
			fmt.Fprintln(os.Stderr, cli.FormatError(err, debugFlag))
		}
		os.Exit(cli.ExitCodeFor(err))
	}
}
