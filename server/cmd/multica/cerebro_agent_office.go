package main

// CEREBRO-PATCH(cli-agent-office): FIR-1775 — `multica agent context` commands
// for the Agent Office: version history, propose a versioned change, review,
// diff, ownership, and rollback for an agent's full runtime context. Mirrors the
// `multica skill` governance commands and backs onto the same REST endpoints the
// web UI and MCP tools use (internal/cerebro/agentoffice/handler.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cerebro/agentoffice"
	"github.com/multica-ai/multica/server/internal/cli"
)

var agentContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Version, review, diff, and roll back an agent's runtime context (Agent Office)",
	Long: `Agent Office (FIR-1775): the agent's full runtime context — instructions,
bound skills, model, mcp_config, and more — is versioned, reviewable, and
reversible, mirroring skill governance.

To change an agent's context you do not own, PROPOSE a versioned change and let
the owner review it, rather than editing the agent directly.`,
}

var agentContextVersionsCmd = &cobra.Command{
	Use:   "versions <agent-id>",
	Short: "List an agent's context version history (newest first)",
	Args:  exactArgs(1),
	RunE:  runAgentContextVersions,
}

var agentContextProposeCmd = &cobra.Command{
	Use:   "propose <agent-id>",
	Short: "Propose a versioned change to an agent's context",
	Args:  exactArgs(1),
	RunE:  runAgentContextPropose,
}

var agentContextChangeRequestsCmd = &cobra.Command{
	Use:   "change-requests [agent-id]",
	Short: "List context change requests for an agent, or the workspace pending queue",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAgentContextChangeRequests,
}

var agentContextReviewCmd = &cobra.Command{
	Use:   "review <change-request-id>",
	Short: "Approve or reject a pending agent-context change request",
	Args:  exactArgs(1),
	RunE:  runAgentContextReview,
}

var agentContextDiffCmd = &cobra.Command{
	Use:   "diff <agent-id>",
	Short: "Show a unified diff between two versions of an agent's context",
	Args:  exactArgs(1),
	RunE:  runAgentContextDiff,
}

var agentContextOwnershipCmd = &cobra.Command{
	Use:   "ownership <agent-id>",
	Short: "Set the context owner and/or approvers for an agent",
	Args:  exactArgs(1),
	RunE:  runAgentContextOwnership,
}

var agentContextRollbackCmd = &cobra.Command{
	Use:   "rollback <agent-id>",
	Short: "Propose rolling an agent's context back to a historical version (reviewed before it applies)",
	Args:  exactArgs(1),
	RunE:  runAgentContextRollback,
}

var agentContextRegionSyncCmd = &cobra.Command{
	Use:   "region-sync <file>",
	Short: "Write or update the Agent Office managed region in a repo CLAUDE.md/AGENTS.md",
	Long: `Managed region (FIR-1775, docs/agents/agent-office.md §10). The region keeps
Agent Office-governed guidance inside the repo file between markers:

  <!-- agent-office:start vN sha256:... -->
  ...governed body...
  <!-- agent-office:end -->

This command is the ONLY sanctioned way to edit that region: it replaces the
body, recomputes the seal, and bumps the version (existing+1, or 1 when the
file has no region yet). Content outside the markers is never touched. CI
(scripts/validate-agent-office-region.sh) rejects hand-edited regions.`,
	Args: exactArgs(1),
	RunE: runAgentContextRegionSync,
}

var agentContextRegionVerifyCmd = &cobra.Command{
	Use:   "region-verify <file>",
	Short: "Verify the Agent Office managed region seal in a repo CLAUDE.md/AGENTS.md",
	Args:  exactArgs(1),
	RunE:  runAgentContextRegionVerify,
}

var agentContextLintCmd = &cobra.Command{
	Use:   "lint [agent-id]",
	Short: "Drift-lint an agent's context (or the whole workspace), and repo CLAUDE.md/AGENTS.md files",
	Long: `Context lint (FIR-1775 Phase 3). Three modes:

  multica agent context lint <agent-id>        one agent: dead/unbound skill
                                               refs, duplicated rules across
                                               layers, empty governance fields,
                                               stale repo links
  multica agent context lint                   workspace-wide sweep over every
                                               non-archived agent
  multica agent context lint --repo-file <p>   drift-lint a repo CLAUDE.md /
                                               AGENTS.md: flags agent-behavior
                                               language and lines duplicated
                                               from the versioned harness
                                               (repeatable)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentContextLint,
}

func init() {
	agentCmd.AddCommand(agentContextCmd)
	agentContextCmd.AddCommand(agentContextVersionsCmd)
	agentContextCmd.AddCommand(agentContextProposeCmd)
	agentContextCmd.AddCommand(agentContextChangeRequestsCmd)
	agentContextCmd.AddCommand(agentContextReviewCmd)
	agentContextCmd.AddCommand(agentContextDiffCmd)
	agentContextCmd.AddCommand(agentContextOwnershipCmd)
	agentContextCmd.AddCommand(agentContextRollbackCmd)
	agentContextCmd.AddCommand(agentContextLintCmd)
	agentContextCmd.AddCommand(agentContextRegionSyncCmd)
	agentContextCmd.AddCommand(agentContextRegionVerifyCmd)

	agentContextVersionsCmd.Flags().String("output", "json", "Output format: table or json")

	agentContextProposeCmd.Flags().String("title", "", "Short title / reason for the proposal (required)")
	agentContextProposeCmd.Flags().String("description", "", "Longer description / context for reviewers")
	agentContextProposeCmd.Flags().String("version", "", "Proposed semver X.Y.Z (must be > current; required)")
	agentContextProposeCmd.Flags().String("instructions", "", "Full replacement instructions (mutually exclusive with --instructions-file)")
	agentContextProposeCmd.Flags().String("instructions-file", "", "Read replacement instructions from a local file")
	agentContextProposeCmd.Flags().String("model", "", "Model override")
	agentContextProposeCmd.Flags().String("thinking-level", "", "Thinking level override")
	agentContextProposeCmd.Flags().String("persona-sandbox", "", "Persona sandbox override")
	agentContextProposeCmd.Flags().StringArray("skill-id", nil, "Full replacement set of bound skill ids (repeatable)")
	agentContextProposeCmd.Flags().Bool("replace-skills", false, "Apply the --skill-id set even if empty (clears bindings)")
	agentContextProposeCmd.Flags().String("mcp-config", "", "Full replacement MCP config as a JSON object string (mutually exclusive with --mcp-config-file)")
	agentContextProposeCmd.Flags().String("mcp-config-file", "", "Read the replacement MCP config JSON from a local file")
	agentContextProposeCmd.Flags().String("output", "json", "Output format: json")

	agentContextChangeRequestsCmd.Flags().Bool("pending", false, "Workspace-wide: only pending change requests (ignores agent-id)")
	agentContextChangeRequestsCmd.Flags().String("output", "table", "Output format: table or json")

	agentContextReviewCmd.Flags().Bool("approve", false, "Approve and merge the change request")
	agentContextReviewCmd.Flags().Bool("reject", false, "Reject the change request")
	agentContextReviewCmd.Flags().String("comment", "", "Review comment (shown to the proposer)")
	agentContextReviewCmd.Flags().String("output", "json", "Output format: json")

	agentContextDiffCmd.Flags().String("from", "", "Version to diff from, e.g. 1.0.0 (required)")
	agentContextDiffCmd.Flags().String("to", "", "Version to diff to, e.g. 1.1.0 (defaults to the live context)")
	agentContextDiffCmd.Flags().String("output", "table", "Output format: table (text diff) or json")

	agentContextOwnershipCmd.Flags().String("owner-id", "", "New context owner by canonical UUID (empty string clears)")
	agentContextOwnershipCmd.Flags().StringArray("approver-id", nil, "Approver by canonical UUID (repeatable)")
	agentContextOwnershipCmd.Flags().Bool("clear-approvers", false, "Set the approver list to empty")
	agentContextOwnershipCmd.Flags().String("output", "json", "Output format: json")

	agentContextRollbackCmd.Flags().String("version", "", "Historical version to roll back to, e.g. 1.0.0 (required)")
	agentContextRollbackCmd.Flags().String("comment", "", "Optional note describing why (becomes the change request description)")
	agentContextRollbackCmd.Flags().String("output", "json", "Output format: json")

	agentContextLintCmd.Flags().StringArray("repo-file", nil, "Path to a repo CLAUDE.md/AGENTS.md to drift-lint (repeatable)")
	agentContextLintCmd.Flags().String("output", "json", "Output format: json")

	agentContextRegionSyncCmd.Flags().String("content-file", "", "Read the new region body from a local file (mutually exclusive with --content-stdin)")
	agentContextRegionSyncCmd.Flags().Bool("content-stdin", false, "Read the new region body from stdin")
	agentContextRegionSyncCmd.Flags().Int("version", 0, "Explicit region version (default: existing version + 1, or 1)")
	agentContextRegionSyncCmd.Flags().String("output", "json", "Output format: json")

	agentContextRegionVerifyCmd.Flags().String("output", "json", "Output format: json")
}

func runAgentContextVersions(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out any
	if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/context/versions", &out); err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runAgentContextPropose(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	version, _ := cmd.Flags().GetString("version")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("--version is required (semver X.Y.Z, greater than current)")
	}

	body := map[string]any{"title": title, "proposed_version": version}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}

	instructions, _ := cmd.Flags().GetString("instructions")
	instructionsFile, _ := cmd.Flags().GetString("instructions-file")
	if instructions != "" && instructionsFile != "" {
		return fmt.Errorf("--instructions and --instructions-file are mutually exclusive")
	}
	if instructionsFile != "" {
		data, err := os.ReadFile(instructionsFile)
		if err != nil {
			return fmt.Errorf("read instructions-file: %w", err)
		}
		instructions = string(data)
	}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if v, _ := cmd.Flags().GetString("model"); v != "" {
		body["model"] = v
	}
	if v, _ := cmd.Flags().GetString("thinking-level"); v != "" {
		body["thinking_level"] = v
	}
	if v, _ := cmd.Flags().GetString("persona-sandbox"); v != "" {
		body["persona_sandbox"] = v
	}
	skillIDs, _ := cmd.Flags().GetStringArray("skill-id")
	replaceSkills, _ := cmd.Flags().GetBool("replace-skills")
	if len(skillIDs) > 0 || replaceSkills {
		body["skill_ids"] = skillIDs
	}

	mcpConfig, _ := cmd.Flags().GetString("mcp-config")
	mcpConfigFile, _ := cmd.Flags().GetString("mcp-config-file")
	if mcpConfig != "" && mcpConfigFile != "" {
		return fmt.Errorf("--mcp-config and --mcp-config-file are mutually exclusive")
	}
	if mcpConfigFile != "" {
		data, err := os.ReadFile(mcpConfigFile)
		if err != nil {
			return fmt.Errorf("read mcp-config-file: %w", err)
		}
		mcpConfig = string(data)
	}
	if strings.TrimSpace(mcpConfig) != "" {
		// Validate locally so a malformed config fails here with a clear message
		// rather than as an opaque server error; forward the raw JSON verbatim.
		if !json.Valid([]byte(mcpConfig)) {
			return fmt.Errorf("--mcp-config is not valid JSON")
		}
		body["mcp_config"] = json.RawMessage(mcpConfig)
	}

	if taskID := strings.TrimSpace(os.Getenv("MULTICA_TASK_ID")); taskID != "" {
		body["work_session_id"] = taskID
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/context/change-requests", body, &out); err != nil {
		return fmt.Errorf("propose change request: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runAgentContextChangeRequests(cmd *cobra.Command, args []string) error {
	pending, _ := cmd.Flags().GetBool("pending")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	path := "/api/agents/context/change-requests"
	if !pending {
		if len(args) == 0 {
			return fmt.Errorf("provide an <agent-id>, or pass --pending for the workspace queue")
		}
		path = "/api/agents/" + url.PathEscape(args[0]) + "/context/change-requests"
	}
	var out any
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return fmt.Errorf("list change requests: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runAgentContextReview(cmd *cobra.Command, args []string) error {
	approve, _ := cmd.Flags().GetBool("approve")
	reject, _ := cmd.Flags().GetBool("reject")
	if approve == reject {
		return fmt.Errorf("pass exactly one of --approve or --reject")
	}
	action := "approve"
	if reject {
		action = "reject"
	}
	body := map[string]any{"action": action}
	if v, _ := cmd.Flags().GetString("comment"); v != "" {
		body["comment"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, "/api/agents/context/change-requests/"+url.PathEscape(args[0])+"/review", body, &out); err != nil {
		return fmt.Errorf("review change request: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runAgentContextDiff(cmd *cobra.Command, args []string) error {
	from, _ := cmd.Flags().GetString("from")
	if strings.TrimSpace(from) == "" {
		return fmt.Errorf("--from is required")
	}
	to, _ := cmd.Flags().GetString("to")
	q := url.Values{}
	q.Set("from", from)
	if to != "" {
		q.Set("to", to)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, _ := cmd.Flags().GetString("output")
	var out struct {
		Diff string `json:"diff"`
	}
	var raw any
	if output == "json" {
		if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/context/diff?"+q.Encode(), &raw); err != nil {
			return fmt.Errorf("diff: %w", err)
		}
		return cli.PrintJSON(os.Stdout, raw)
	}
	if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/context/diff?"+q.Encode(), &out); err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	if out.Diff == "" {
		fmt.Println("(no differences)")
		return nil
	}
	fmt.Print(out.Diff)
	return nil
}

func runAgentContextOwnership(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if cmd.Flags().Changed("owner-id") {
		v, _ := cmd.Flags().GetString("owner-id")
		body["owner_id"] = v
	}
	approverIDs, _ := cmd.Flags().GetStringArray("approver-id")
	clearApprovers, _ := cmd.Flags().GetBool("clear-approvers")
	if len(approverIDs) > 0 || clearApprovers {
		body["approver_ids"] = approverIDs
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update: pass --owner-id and/or --approver-id (or --clear-approvers)")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out any
	if err := client.PutJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/context/ownership", body, &out); err != nil {
		return fmt.Errorf("update ownership: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runAgentContextLint(cmd *cobra.Command, args []string) error {
	repoFiles, _ := cmd.Flags().GetStringArray("repo-file")
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if len(repoFiles) > 0 {
		// Repo-file drift lint: the server has no checkout of the repo, so the
		// CLI reads each file and posts its content to be checked against the
		// workspace harness (agent instructions + skills).
		results := make([]any, 0, len(repoFiles))
		for _, path := range repoFiles {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read repo-file: %w", err)
			}
			body := map[string]any{"filename": path, "content": string(data)}
			var out any
			if err := client.PostJSON(ctx, "/api/agents/context/lint/repo-file", body, &out); err != nil {
				return fmt.Errorf("lint repo-file %s: %w", path, err)
			}
			results = append(results, out)
		}
		if len(results) == 1 {
			return cli.PrintJSON(os.Stdout, results[0])
		}
		return cli.PrintJSON(os.Stdout, results)
	}

	path := "/api/agents/context/lint"
	if len(args) == 1 {
		path = "/api/agents/" + url.PathEscape(args[0]) + "/context/lint"
	}
	var out any
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return fmt.Errorf("lint: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runAgentContextRegionSync(cmd *cobra.Command, args []string) error {
	path := args[0]
	contentFile, _ := cmd.Flags().GetString("content-file")
	contentStdin, _ := cmd.Flags().GetBool("content-stdin")
	if (contentFile == "") == !contentStdin {
		return fmt.Errorf("provide the new region body via exactly one of --content-file or --content-stdin")
	}
	var body []byte
	var err error
	if contentStdin {
		body, err = io.ReadAll(os.Stdin)
	} else {
		body, err = os.ReadFile(contentFile)
	}
	if err != nil {
		return fmt.Errorf("read region body: %w", err)
	}

	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	region, err := agentoffice.ParseManagedRegion(existing)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	version, _ := cmd.Flags().GetInt("version")
	previousVersion := 0
	if region != nil {
		previousVersion = region.Version
	}
	if version == 0 {
		version = previousVersion + 1
	}
	if version <= previousVersion {
		return fmt.Errorf("--version must be greater than the existing region version v%d", previousVersion)
	}

	updated, err := agentoffice.UpsertManagedRegion(existing, version, string(body))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return cli.PrintJSON(os.Stdout, map[string]any{
		"file":             path,
		"version":          version,
		"previous_version": previousVersion,
		"created":          region == nil,
	})
}

func runAgentContextRegionVerify(cmd *cobra.Command, args []string) error {
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	region, err := agentoffice.ParseManagedRegion(string(data))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if region == nil {
		return cli.PrintJSON(os.Stdout, map[string]any{"file": path, "region": false, "valid": true})
	}
	if !region.Verify() {
		_ = cli.PrintJSON(os.Stdout, map[string]any{"file": path, "region": true, "valid": false, "version": region.Version})
		return fmt.Errorf("%s: managed-region seal is broken — the body was edited without 'multica agent context region-sync'", path)
	}
	return cli.PrintJSON(os.Stdout, map[string]any{"file": path, "region": true, "valid": true, "version": region.Version})
}

func runAgentContextRollback(cmd *cobra.Command, args []string) error {
	version, _ := cmd.Flags().GetString("version")
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("--version is required (the historical version to restore)")
	}
	body := map[string]any{"version": version}
	if v, _ := cmd.Flags().GetString("comment"); v != "" {
		body["comment"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, "/api/agents/"+url.PathEscape(args[0])+"/context/rollback", body, &out); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}
	return cli.PrintJSON(os.Stdout, out)
}
