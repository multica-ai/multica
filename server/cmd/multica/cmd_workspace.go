package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Work with workspaces",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces you belong to",
	RunE:  runWorkspaceList,
}

var workspaceGetCmd = &cobra.Command{
	Use:   "get [workspace-id]",
	Short: "Get workspace details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceGet,
}

var workspaceMembersCmd = &cobra.Command{
	Use:   "members [workspace-id]",
	Short: "List workspace members",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceMembers,
}

var workspaceWatchCmd = &cobra.Command{
	Use:   "watch <workspace-id>",
	Short: "Add a workspace to the daemon watch list",
	Args:  exactArgs(1),
	RunE:  runWatch,
}

var workspaceUnwatchCmd = &cobra.Command{
	Use:   "unwatch <workspace-id>",
	Short: "Remove a workspace from the daemon watch list",
	Args:  exactArgs(1),
	RunE:  runUnwatch,
}

var workspaceSkillsSyncCmd = &cobra.Command{
	Use:   "skills-sync",
	Short: "Configure workspace skill synchronization",
}

var workspaceSkillsSyncSetCmd = &cobra.Command{
	Use:   "set <workspace-id>",
	Short: "Configure the local skills directory for a watched workspace",
	Args:  exactArgs(1),
	RunE:  runWorkspaceSkillsSyncSet,
}

var workspaceSkillsSyncStatusCmd = &cobra.Command{
	Use:   "status [workspace-id]",
	Short: "Show skill sync status for a watched workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceSkillsSyncStatus,
}

var workspaceSkillsSyncDisableCmd = &cobra.Command{
	Use:   "disable <workspace-id>",
	Short: "Disable automatic skill sync for a watched workspace",
	Args:  exactArgs(1),
	RunE:  runWorkspaceSkillsSyncDisable,
}

var workspaceSkillsSyncRunCmd = &cobra.Command{
	Use:   "run [workspace-id]",
	Short: "Run one immediate skill sync for a watched workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceSkillsSyncRun,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMembersCmd)
	workspaceCmd.AddCommand(workspaceWatchCmd)
	workspaceCmd.AddCommand(workspaceUnwatchCmd)
	workspaceCmd.AddCommand(workspaceSkillsSyncCmd)

	workspaceSkillsSyncCmd.AddCommand(workspaceSkillsSyncSetCmd)
	workspaceSkillsSyncCmd.AddCommand(workspaceSkillsSyncStatusCmd)
	workspaceSkillsSyncCmd.AddCommand(workspaceSkillsSyncDisableCmd)
	workspaceSkillsSyncCmd.AddCommand(workspaceSkillsSyncRunCmd)

	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMembersCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceSkillsSyncSetCmd.Flags().String("dir", "", "Absolute or relative path to the local skills directory")
	workspaceSkillsSyncSetCmd.Flags().Bool("delete-managed", false, "Delete daemon-managed skills missing from the local directory during sync")
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "No workspaces found.")
		return nil
	}

	// Load watched set for marking.
	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	watched := make(map[string]bool)
	for _, w := range cfg.WatchedWorkspaces {
		watched[w.ID] = true
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tWATCHING")
	for _, ws := range workspaces {
		mark := ""
		if watched[ws.ID] {
			mark = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", ws.ID, ws.Name, mark)
	}
	return w.Flush()
}

func workspaceIDFromArgs(cmd *cobra.Command, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return resolveWorkspaceID(cmd)
}

func runWorkspaceGet(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		desc := strVal(ws, "description")
		if utf8.RuneCountInString(desc) > 60 {
			runes := []rune(desc)
			desc = string(runes[:57]) + "..."
		}
		wsContext := strVal(ws, "context")
		if utf8.RuneCountInString(wsContext) > 60 {
			runes := []rune(wsContext)
			wsContext = string(runes[:57]) + "..."
		}
		headers := []string{"ID", "NAME", "SLUG", "DESCRIPTION", "CONTEXT"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
			desc,
			wsContext,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

func runWorkspaceMembers(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	headers := []string{"USER ID", "NAME", "EMAIL", "ROLE"}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, []string{
			strVal(m, "user_id"),
			strVal(m, "name"),
			strVal(m, "email"),
			strVal(m, "role"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	workspaceID := args[0]

	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID, &ws); err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	profile := resolveProfile(cmd)
	var added bool
	var setDefault bool
	if err := cli.UpdateCLIConfigForProfile(profile, func(cfg *cli.CLIConfig) error {
		added = cfg.AddWatchedWorkspace(ws.ID, ws.Name)
		if !added {
			return nil
		}
		if cfg.WorkspaceID == "" {
			cfg.WorkspaceID = ws.ID
			setDefault = true
		}
		return nil
	}); err != nil {
		return err
	}
	if !added {
		fmt.Fprintf(os.Stderr, "Already watching workspace %s (%s)\n", ws.ID, ws.Name)
		return nil
	}
	if setDefault {
		fmt.Fprintf(os.Stderr, "Set default workspace to %s (%s)\n", ws.ID, ws.Name)
	}

	fmt.Fprintf(os.Stderr, "Watching workspace %s (%s)\n", ws.ID, ws.Name)
	return nil
}

func runUnwatch(cmd *cobra.Command, args []string) error {
	workspaceID := args[0]

	profile := resolveProfile(cmd)
	var removed bool
	if err := cli.UpdateCLIConfigForProfile(profile, func(cfg *cli.CLIConfig) error {
		removed = cfg.RemoveWatchedWorkspace(workspaceID)
		return nil
	}); err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("workspace %s is not being watched", workspaceID)
	}

	fmt.Fprintf(os.Stderr, "Stopped watching workspace %s\n", workspaceID)
	return nil
}

func runWorkspaceSkillsSyncSet(cmd *cobra.Command, args []string) error {
	workspaceID := args[0]
	dirFlag := strings.TrimSpace(flagString(cmd, "dir"))
	if dirFlag == "" {
		return fmt.Errorf("--dir is required")
	}

	absDir, err := filepath.Abs(dirFlag)
	if err != nil {
		return fmt.Errorf("resolve skills directory %q: %w", dirFlag, err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("stat skills directory %q: %w", absDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("skills directory %q is not a directory", absDir)
	}

	profile := resolveProfile(cmd)
	if err := cli.UpdateCLIConfigForProfile(profile, func(cfg *cli.CLIConfig) error {
		for i := range cfg.WatchedWorkspaces {
			if cfg.WatchedWorkspaces[i].ID != workspaceID {
				continue
			}
			if cfg.WatchedWorkspaces[i].SkillSync == nil {
				cfg.WatchedWorkspaces[i].SkillSync = &cli.WorkspaceSkillSync{}
			}
			dirChanged := cfg.WatchedWorkspaces[i].SkillSync.Dir != "" && cfg.WatchedWorkspaces[i].SkillSync.Dir != absDir
			deleteManaged := cfg.WatchedWorkspaces[i].SkillSync.DeleteManaged
			if cmd.Flags().Changed("delete-managed") {
				deleteManaged, _ = cmd.Flags().GetBool("delete-managed")
			}
			cfg.WatchedWorkspaces[i].SkillSync.Dir = absDir
			cfg.WatchedWorkspaces[i].SkillSync.Enabled = true
			cfg.WatchedWorkspaces[i].SkillSync.DeleteManaged = deleteManaged
			if dirChanged {
				cfg.WatchedWorkspaces[i].SkillSync.LastSyncAt = ""
				cfg.WatchedWorkspaces[i].SkillSync.LastSyncError = ""
			}
			return nil
		}
		return fmt.Errorf("workspace %s is not being watched: run 'multica workspace watch %s' first", workspaceID, workspaceID)
	}); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Configured skill sync for workspace %s\n", workspaceID)
	return nil
}

func runWorkspaceSkillsSyncStatus(cmd *cobra.Command, args []string) error {
	workspaceID, err := workspaceIDFromArgsOrDefault(cmd, args)
	if err != nil {
		return err
	}

	profile := resolveProfile(cmd)
	_, watched, err := loadWatchedWorkspaceConfig(profile, workspaceID)
	if err != nil {
		return err
	}

	syncState := watched.SkillSync
	enabled := false
	dir := ""
	deleteManaged := false
	lastSyncAt := ""
	lastSyncError := ""
	if syncState != nil {
		enabled = syncState.Enabled
		dir = syncState.Dir
		deleteManaged = syncState.DeleteManaged
		lastSyncAt = syncState.LastSyncAt
		lastSyncError = syncState.LastSyncError
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "workspace_id:\t%s\n", workspaceID)
	fmt.Fprintf(w, "enabled:\t%t\n", enabled)
	fmt.Fprintf(w, "dir:\t%s\n", dir)
	fmt.Fprintf(w, "delete_managed:\t%t\n", deleteManaged)
	fmt.Fprintf(w, "last_sync_at:\t%s\n", lastSyncAt)
	fmt.Fprintf(w, "last_sync_error:\t%s\n", lastSyncError)
	return w.Flush()
}

func runWorkspaceSkillsSyncDisable(cmd *cobra.Command, args []string) error {
	workspaceID := args[0]
	profile := resolveProfile(cmd)
	if err := cli.UpdateCLIConfigForProfile(profile, func(cfg *cli.CLIConfig) error {
		for i := range cfg.WatchedWorkspaces {
			if cfg.WatchedWorkspaces[i].ID != workspaceID {
				continue
			}
			if cfg.WatchedWorkspaces[i].SkillSync == nil {
				cfg.WatchedWorkspaces[i].SkillSync = &cli.WorkspaceSkillSync{}
			}
			cfg.WatchedWorkspaces[i].SkillSync.Enabled = false
			return nil
		}
		return fmt.Errorf("workspace %s is not being watched: run 'multica workspace watch %s' first", workspaceID, workspaceID)
	}); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Disabled automatic skill sync for workspace %s\n", workspaceID)
	return nil
}

func runWorkspaceSkillsSyncRun(cmd *cobra.Command, args []string) error {
	workspaceID, err := workspaceIDFromArgsOrDefault(cmd, args)
	if err != nil {
		return err
	}

	profile := resolveProfile(cmd)
	unlockConfig, err := cli.LockCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	defer unlockConfig()

	cfg, watched, err := loadWatchedWorkspaceConfig(profile, workspaceID)
	if err != nil {
		return err
	}
	if watched.SkillSync == nil || strings.TrimSpace(watched.SkillSync.Dir) == "" {
		return fmt.Errorf("workspace %s does not have skill sync configured: run 'multica workspace skills-sync set %s --dir <path>' first", workspaceID, workspaceID)
	}

	syncDir := watched.SkillSync.Dir
	info, err := os.Stat(syncDir)
	if err != nil {
		return saveLockedWorkspaceSkillSyncStatus(cfg, profile, workspaceID, time.Time{}, fmt.Errorf("stat skills directory %q: %w", syncDir, err))
	}
	if !info.IsDir() {
		return saveLockedWorkspaceSkillSyncStatus(cfg, profile, workspaceID, time.Time{}, fmt.Errorf("skills directory %q is not a directory", syncDir))
	}

	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	localSkills, err := daemon.ScanLocalSkills(syncDir)
	if err != nil {
		return saveLockedWorkspaceSkillSyncStatus(cfg, profile, workspaceID, time.Time{}, fmt.Errorf("scan local skills: %w", err))
	}

	client := daemon.NewClient(resolveServerURL(cmd))
	client.SetToken(token)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := daemon.ReconcileWorkspaceSkills(ctx, client, daemon.WorkspaceSkillSyncRequest{
		WorkspaceID:   workspaceID,
		SyncDir:       syncDir,
		DaemonID:      deriveWorkspaceSkillSyncDaemonID(profile),
		Profile:       profile,
		DeleteManaged: watched.SkillSync.DeleteManaged,
		LocalSkills:   localSkills,
	})
	if err != nil {
		return saveLockedWorkspaceSkillSyncStatus(cfg, profile, workspaceID, time.Time{}, err)
	}

	if err := saveLockedWorkspaceSkillSyncStatus(cfg, profile, workspaceID, time.Now(), nil); err != nil {
		return err
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "workspace_id:\t%s\n", workspaceID)
	fmt.Fprintf(w, "dir:\t%s\n", syncDir)
	fmt.Fprintf(w, "created:\t%d\n", len(result.Created))
	fmt.Fprintf(w, "updated:\t%d\n", len(result.Updated))
	fmt.Fprintf(w, "deleted:\t%d\n", len(result.Deleted))
	fmt.Fprintf(w, "unchanged:\t%d\n", len(result.Unchanged))
	fmt.Fprintf(w, "conflicts:\t%d\n", len(result.Conflicts))
	if len(result.Conflicts) > 0 {
		fmt.Fprintf(w, "conflict_names:\t%s\n", strings.Join(result.Conflicts, ", "))
	}
	return w.Flush()
}

func workspaceIDFromArgsOrDefault(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	return requireWorkspaceID(cmd)
}

func loadWatchedWorkspaceConfig(profile, workspaceID string) (*cli.CLIConfig, *cli.WatchedWorkspace, error) {
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return nil, nil, err
	}

	for i := range cfg.WatchedWorkspaces {
		if cfg.WatchedWorkspaces[i].ID == workspaceID {
			return &cfg, &cfg.WatchedWorkspaces[i], nil
		}
	}

	return nil, nil, fmt.Errorf("workspace %s is not being watched: run 'multica workspace watch %s' first", workspaceID, workspaceID)
}

func markWorkspaceSkillSyncFailure(workspaceID, profile string, syncErr error) error {
	if err := cli.UpdateWorkspaceSkillSyncStatus(profile, workspaceID, time.Time{}, syncErr); err != nil {
		return fmt.Errorf("%v (also failed to save skill sync error: %w)", syncErr, err)
	}
	return syncErr
}

func saveLockedWorkspaceSkillSyncStatus(cfg *cli.CLIConfig, profile, workspaceID string, syncedAt time.Time, syncErr error) error {
	for i := range cfg.WatchedWorkspaces {
		if cfg.WatchedWorkspaces[i].ID != workspaceID {
			continue
		}
		if cfg.WatchedWorkspaces[i].SkillSync == nil {
			cfg.WatchedWorkspaces[i].SkillSync = &cli.WorkspaceSkillSync{}
		}
		if syncErr != nil {
			cfg.WatchedWorkspaces[i].SkillSync.LastSyncError = syncErr.Error()
		} else {
			cfg.WatchedWorkspaces[i].SkillSync.LastSyncAt = syncedAt.UTC().Format(time.RFC3339)
			cfg.WatchedWorkspaces[i].SkillSync.LastSyncError = ""
		}
		if err := cli.SaveCLIConfigForProfile(*cfg, profile); err != nil {
			if syncErr != nil {
				return fmt.Errorf("%v (also failed to save skill sync error: %w)", syncErr, err)
			}
			return err
		}
		if syncErr != nil {
			return syncErr
		}
		return nil
	}

	if syncErr != nil {
		return fmt.Errorf("%v (also failed to save skill sync error: workspace %s is not being watched)", syncErr, workspaceID)
	}
	return fmt.Errorf("workspace %s is not being watched", workspaceID)
}

func deriveWorkspaceSkillSyncDaemonID(profile string) string {
	if daemonID := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_ID")); daemonID != "" {
		return daemonID
	}

	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	if profile == "" {
		return host
	}
	return host + "/" + profile
}
