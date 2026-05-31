// CEREBRO-PATCH(skill-cli-governance): FIR-2628 — CLI flow for skill
// ownership, change requests, version history / diff, and forks. Wraps the
// already-live /api/skills/... endpoints added in JEH-216 so agents can drive
// the ownership workflow without going through the web UI.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Command tree — registered onto skillCmd from cmd_skill.go.
// ---------------------------------------------------------------------------

var skillProposeCmd = &cobra.Command{
	Use:   "propose <id>",
	Short: "Propose a change to a skill (opens a change request)",
	Args:  exactArgs(1),
	RunE:  runSkillPropose,
}

var skillChangeRequestsCmd = &cobra.Command{
	Use:   "change-requests",
	Short: "List skill change requests (per-skill or workspace-pending)",
	RunE:  runSkillChangeRequestsList,
}

var skillReviewCmd = &cobra.Command{
	Use:   "review <crId>",
	Short: "Approve or reject a skill change request",
	Args:  exactArgs(1),
	RunE:  runSkillReview,
}

var skillVersionsCmd = &cobra.Command{
	Use:   "versions <id>",
	Short: "List version history for a skill",
	Args:  exactArgs(1),
	RunE:  runSkillVersions,
}

var skillDiffCmd = &cobra.Command{
	Use:   "diff <id>",
	Short: "Show a line diff between two versions of a skill",
	Args:  exactArgs(1),
	RunE:  runSkillDiff,
}

var skillOwnershipCmd = &cobra.Command{
	Use:   "ownership <id>",
	Short: "Set owner / approvers on a skill",
	Args:  exactArgs(1),
	RunE:  runSkillOwnership,
}

var skillForkCmd = &cobra.Command{
	Use:   "fork <id>",
	Short: "Fork a skill into a new skill you own",
	Args:  exactArgs(1),
	RunE:  runSkillFork,
}

func init() {
	skillCmd.AddCommand(skillProposeCmd)
	skillCmd.AddCommand(skillChangeRequestsCmd)
	skillCmd.AddCommand(skillReviewCmd)
	skillCmd.AddCommand(skillVersionsCmd)
	skillCmd.AddCommand(skillDiffCmd)
	skillCmd.AddCommand(skillOwnershipCmd)
	skillCmd.AddCommand(skillForkCmd)

	// skill propose
	skillProposeCmd.Flags().String("title", "", "Short title / reason for the proposal (required)")
	skillProposeCmd.Flags().String("description", "", "Longer description / context for reviewers")
	skillProposeCmd.Flags().String("version", "", "Proposed semver X.Y.Z (must be > current; required)")
	skillProposeCmd.Flags().String("content", "", "Proposed SKILL.md body (mutually exclusive with --content-file)")
	skillProposeCmd.Flags().String("content-file", "", "Read proposed SKILL.md body from a local file")
	skillProposeCmd.Flags().StringArray("file", nil, "Proposed file as 'skill-path=local-path' or 'local-path' (repeatable)")
	skillProposeCmd.Flags().String("output", "json", "Output format: table or json")

	// skill change-requests
	skillChangeRequestsCmd.Flags().String("skill", "", "Limit to a single skill (by skill id)")
	skillChangeRequestsCmd.Flags().Bool("pending", false, "Workspace-wide: only pending change requests")
	skillChangeRequestsCmd.Flags().String("output", "table", "Output format: table or json")

	// skill review
	skillReviewCmd.Flags().Bool("approve", false, "Approve and merge the change request")
	skillReviewCmd.Flags().Bool("reject", false, "Reject the change request")
	skillReviewCmd.Flags().String("comment", "", "Review comment (shown to the proposer)")
	skillReviewCmd.Flags().String("output", "json", "Output format: table or json")

	// skill versions
	skillVersionsCmd.Flags().String("output", "table", "Output format: table or json")

	// skill diff
	skillDiffCmd.Flags().String("from", "", "Version to diff from, e.g. 1.0.0 (required)")
	skillDiffCmd.Flags().String("to", "", "Version to diff to, e.g. 1.1.0 (required)")
	skillDiffCmd.Flags().String("output", "table", "Output format: table (text diff) or json (per-file)")

	// skill ownership
	skillOwnershipCmd.Flags().String("owner", "", "New owner by name (member or agent)")
	skillOwnershipCmd.Flags().String("owner-id", "", "New owner by canonical UUID (mutually exclusive with --owner)")
	skillOwnershipCmd.Flags().StringArray("approver", nil, "Approver by name (repeatable; member or agent)")
	skillOwnershipCmd.Flags().StringArray("approver-id", nil, "Approver by canonical UUID (repeatable)")
	skillOwnershipCmd.Flags().Bool("clear-approvers", false, "Set the approver list to empty (otherwise the existing list is preserved if no --approver flags are passed)")
	skillOwnershipCmd.Flags().String("output", "json", "Output format: table or json")

	// skill fork
	skillForkCmd.Flags().String("name", "", "Name of the forked skill (required, must be unique in the workspace)")
	skillForkCmd.Flags().String("description", "", "Description for the fork (defaults to the parent's description)")
	skillForkCmd.Flags().String("owner", "", "Initial owner by name (defaults to the caller)")
	skillForkCmd.Flags().String("owner-id", "", "Initial owner by canonical UUID (mutually exclusive with --owner)")
	skillForkCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Propose
// ---------------------------------------------------------------------------

func runSkillPropose(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	version, _ := cmd.Flags().GetString("version")
	if version == "" {
		return fmt.Errorf("--version is required (proposed semver X.Y.Z)")
	}

	content, _ := cmd.Flags().GetString("content")
	contentFile, _ := cmd.Flags().GetString("content-file")
	if content != "" && contentFile != "" {
		return fmt.Errorf("--content and --content-file are mutually exclusive")
	}
	if contentFile != "" {
		raw, err := os.ReadFile(contentFile)
		if err != nil {
			return fmt.Errorf("read --content-file: %w", err)
		}
		content = string(raw)
	}

	fileSpecs, _ := cmd.Flags().GetStringArray("file")
	files, err := parseSkillFileSpecs(fileSpecs)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{
		"title":            title,
		"proposed_version": version,
		"proposed_content": content,
		"proposed_files":   files,
	}
	if desc, _ := cmd.Flags().GetString("description"); desc != "" {
		body["description"] = desc
	}
	// Session reference: when the CLI is running inside a daemon-managed
	// agent run, MULTICA_TASK_ID is set. Forward it as work_session_id so
	// the server (once FIR-2627 lands the column on skill_change_request)
	// can link the change request back to the run that proposed it. The
	// field is harmless to send today — current handler ignores unknown
	// JSON fields, so this propose call works against pre- and
	// post-FIR-2627 servers.
	if taskID := strings.TrimSpace(os.Getenv("MULTICA_TASK_ID")); taskID != "" {
		body["work_session_id"] = taskID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/change-requests", body, &result); err != nil {
		return fmt.Errorf("propose change request: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Change request created: %s\n", strVal(result, "id"))
	fmt.Printf("  skill:    %s\n", strVal(result, "skill_id"))
	fmt.Printf("  version:  %s → %s\n", strVal(result, "base_version"), strVal(result, "proposed_version"))
	fmt.Printf("  status:   %s\n", strVal(result, "status"))
	if title := strVal(result, "title"); title != "" {
		fmt.Printf("  title:    %s\n", title)
	}
	return nil
}

// parseSkillFileSpecs turns repeated --file flags into the [{path, content}]
// shape the API expects. Two forms are accepted:
//
//	--file path/in/skill.md=./local/file.md   explicit mapping
//	--file ./local/file.md                    skill-path defaults to the basename
func parseSkillFileSpecs(specs []string) ([]map[string]string, error) {
	out := make([]map[string]string, 0, len(specs))
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		skillPath, localPath := "", spec
		if eq := strings.Index(spec, "="); eq > 0 {
			skillPath = strings.TrimSpace(spec[:eq])
			localPath = strings.TrimSpace(spec[eq+1:])
		}
		if localPath == "" {
			return nil, fmt.Errorf("--file %q: local path is empty", spec)
		}
		raw, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("--file %q: %w", spec, err)
		}
		if skillPath == "" {
			// Default to the file name (no directory) inside the skill.
			parts := strings.Split(filepathBase(localPath), string(os.PathSeparator))
			skillPath = parts[len(parts)-1]
		}
		out = append(out, map[string]string{"path": skillPath, "content": string(raw)})
	}
	return out, nil
}

// filepathBase returns the last path segment of p, treating both / and
// os.PathSeparator as separators. Kept inline so this file doesn't pull in
// path/filepath for a single call.
func filepathBase(p string) string {
	if p == "" {
		return ""
	}
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || (os.PathSeparator != '/' && p[i] == os.PathSeparator) {
			return p[i+1:]
		}
	}
	return p
}

// ---------------------------------------------------------------------------
// List change requests
// ---------------------------------------------------------------------------

func runSkillChangeRequestsList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	skillID, _ := cmd.Flags().GetString("skill")
	pending, _ := cmd.Flags().GetBool("pending")
	if skillID != "" && pending {
		return fmt.Errorf("--skill and --pending are mutually exclusive (--pending is workspace-scoped)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/skills/change-requests"
	if skillID != "" {
		path = "/api/skills/" + url.PathEscape(skillID) + "/change-requests"
	}
	var rows []map[string]any
	if err := client.GetJSON(ctx, path, &rows); err != nil {
		return fmt.Errorf("list change requests: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}

	headers := []string{"ID", "SKILL", "STATUS", "BASE→PROPOSED", "TITLE", "PROPOSED_BY", "CREATED_AT"}
	out := make([][]string, 0, len(rows))
	for _, c := range rows {
		out = append(out, []string{
			truncateID(strVal(c, "id")),
			truncateID(strVal(c, "skill_id")),
			strVal(c, "status"),
			strVal(c, "base_version") + " → " + strVal(c, "proposed_version"),
			strVal(c, "title"),
			truncateID(strVal(c, "proposed_by")),
			strVal(c, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	return nil
}

// ---------------------------------------------------------------------------
// Review
// ---------------------------------------------------------------------------

func runSkillReview(cmd *cobra.Command, args []string) error {
	approve, _ := cmd.Flags().GetBool("approve")
	reject, _ := cmd.Flags().GetBool("reject")
	if approve == reject {
		return fmt.Errorf("exactly one of --approve or --reject is required")
	}
	action := "approve"
	if reject {
		action = "reject"
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{"action": action}
	if comment, _ := cmd.Flags().GetString("comment"); comment != "" {
		body["comment"] = comment
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	path := "/api/skills/change-requests/" + url.PathEscape(args[0]) + "/review"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("review change request: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Change request %s: %s\n", action+"d", strVal(result, "id"))
	if status := strVal(result, "status"); status != "" {
		fmt.Printf("  status:   %s\n", status)
	}
	if v := strVal(result, "proposed_version"); v != "" {
		fmt.Printf("  version:  %s → %s\n", strVal(result, "base_version"), v)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Versions
// ---------------------------------------------------------------------------

func runSkillVersions(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var rows []map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/versions", &rows); err != nil {
		return fmt.Errorf("list versions: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}

	headers := []string{"VERSION", "DESCRIPTION", "CREATED_BY", "CREATED_AT"}
	out := make([][]string, 0, len(rows))
	for _, v := range rows {
		out = append(out, []string{
			strVal(v, "version"),
			strVal(v, "description"),
			truncateID(strVal(v, "created_by")),
			strVal(v, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
	return nil
}

// ---------------------------------------------------------------------------
// Diff
// ---------------------------------------------------------------------------

func runSkillDiff(cmd *cobra.Command, args []string) error {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	if from == "" || to == "" {
		return fmt.Errorf("--from and --to are both required (semver X.Y.Z)")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var versions []map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/versions", &versions); err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	fromV, ok := findVersion(versions, from)
	if !ok {
		return fmt.Errorf("version %q not found in skill history", from)
	}
	toV, ok := findVersion(versions, to)
	if !ok {
		return fmt.Errorf("version %q not found in skill history", to)
	}

	diffs := diffSkillVersions(fromV, toV)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, diffs)
	}
	printDiffsAsText(os.Stdout, from, to, diffs)
	return nil
}

func findVersion(versions []map[string]any, want string) (map[string]any, bool) {
	for _, v := range versions {
		if strVal(v, "version") == want {
			return v, true
		}
	}
	return nil, false
}

type fileDiff struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
}

func diffSkillVersions(from, to map[string]any) []fileDiff {
	out := []fileDiff{}
	// SKILL.md body lives on the version row as "content".
	fromContent := strVal(from, "content")
	toContent := strVal(to, "content")
	if fromContent != toContent {
		out = append(out, fileDiff{Path: "SKILL.md", Lines: lineDiff(fromContent, toContent)})
	}

	fromFiles := versionFilesByPath(from)
	toFiles := versionFilesByPath(to)
	paths := mergedSortedKeys(fromFiles, toFiles)
	for _, p := range paths {
		a := fromFiles[p]
		b := toFiles[p]
		if a == b {
			continue
		}
		out = append(out, fileDiff{Path: p, Lines: lineDiff(a, b)})
	}
	return out
}

func versionFilesByPath(v map[string]any) map[string]string {
	out := map[string]string{}
	raw, _ := v["files"].([]any)
	for _, item := range raw {
		f, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out[strVal(f, "path")] = strVal(f, "content")
	}
	return out
}

func mergedSortedKeys(a, b map[string]string) []string {
	seen := map[string]struct{}{}
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	// Simple lexical sort; package sort is already imported via cobra deps,
	// but use a stable insertion sort here to avoid pulling sort into this
	// file just for one call.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// lineDiff returns a compact unified-style line diff between a and b. Lines
// present only in a are prefixed "- ", lines only in b with "+ ". Equal lines
// are skipped. The algorithm is LCS-based — fine for SKILL.md-sized files,
// and we never need to diff binary blobs here (skills are markdown / scripts).
func lineDiff(a, b string) []string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")
	la, lb := len(aLines), len(bLines)
	// LCS length table.
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := la - 1; i >= 0; i-- {
		for j := lb - 1; j >= 0; j-- {
			if aLines[i] == bLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	out := make([]string, 0, la+lb)
	i, j := 0, 0
	for i < la && j < lb {
		switch {
		case aLines[i] == bLines[j]:
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, "- "+aLines[i])
			i++
		default:
			out = append(out, "+ "+bLines[j])
			j++
		}
	}
	for ; i < la; i++ {
		out = append(out, "- "+aLines[i])
	}
	for ; j < lb; j++ {
		out = append(out, "+ "+bLines[j])
	}
	return out
}

func printDiffsAsText(w *os.File, from, to string, diffs []fileDiff) {
	if len(diffs) == 0 {
		fmt.Fprintf(w, "No differences between %s and %s.\n", from, to)
		return
	}
	for _, d := range diffs {
		fmt.Fprintf(w, "--- %s @ %s\n", d.Path, from)
		fmt.Fprintf(w, "+++ %s @ %s\n", d.Path, to)
		for _, ln := range d.Lines {
			fmt.Fprintln(w, ln)
		}
		fmt.Fprintln(w)
	}
}

// ---------------------------------------------------------------------------
// Ownership
// ---------------------------------------------------------------------------

func runSkillOwnership(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := map[string]any{}

	// Owner: --owner-id wins if both are set; require workspace lookup
	// when --owner is used. resolveAssignee is the shared name→id resolver,
	// scoped to member-or-agent (skill owners are not squads).
	ownerName, _ := cmd.Flags().GetString("owner")
	ownerID, _ := cmd.Flags().GetString("owner-id")
	if ownerName != "" && ownerID != "" {
		return fmt.Errorf("--owner and --owner-id are mutually exclusive")
	}
	if ownerID != "" {
		body["owner_id"] = ownerID
	} else if ownerName != "" {
		_, resolvedID, err := resolveAssignee(ctx, client, ownerName, memberOrAgentKinds)
		if err != nil {
			return fmt.Errorf("resolve --owner: %w", err)
		}
		body["owner_id"] = resolvedID
	}

	// Approvers: combine --approver names and --approver-id UUIDs into one
	// list, preserving CLI order.
	approverNames, _ := cmd.Flags().GetStringArray("approver")
	approverIDs, _ := cmd.Flags().GetStringArray("approver-id")
	clearApprovers, _ := cmd.Flags().GetBool("clear-approvers")

	approversSet := len(approverNames) > 0 || len(approverIDs) > 0 || clearApprovers
	if approversSet {
		ids := make([]string, 0, len(approverNames)+len(approverIDs))
		for _, name := range approverNames {
			_, id, err := resolveAssignee(ctx, client, name, memberOrAgentKinds)
			if err != nil {
				return fmt.Errorf("resolve --approver %q: %w", name, err)
			}
			ids = append(ids, id)
		}
		ids = append(ids, approverIDs...)
		// Send an empty array to clear; nil would be treated as
		// "leave unchanged" by the handler.
		if ids == nil {
			ids = []string{}
		}
		body["approver_ids"] = ids
	}

	if len(body) == 0 {
		return fmt.Errorf("no changes: provide --owner / --owner-id and/or --approver(s) / --clear-approvers")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/ownership", body, &result); err != nil {
		return fmt.Errorf("update ownership: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Skill ownership updated: %s\n", strVal(result, "id"))
	if owner := strVal(result, "owner_id"); owner != "" {
		fmt.Printf("  owner:     %s\n", owner)
	}
	if approvers, ok := result["approver_ids"].([]any); ok {
		fmt.Printf("  approvers: %d\n", len(approvers))
		for _, a := range approvers {
			if s, ok := a.(string); ok {
				fmt.Printf("    - %s\n", s)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fork
// ---------------------------------------------------------------------------

func runSkillFork(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{"name": name}
	if desc, _ := cmd.Flags().GetString("description"); desc != "" {
		body["description"] = desc
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ownerName, _ := cmd.Flags().GetString("owner")
	ownerID, _ := cmd.Flags().GetString("owner-id")
	if ownerName != "" && ownerID != "" {
		return fmt.Errorf("--owner and --owner-id are mutually exclusive")
	}
	if ownerID != "" {
		body["owner_id"] = ownerID
	} else if ownerName != "" {
		_, resolvedID, err := resolveAssignee(ctx, client, ownerName, memberOrAgentKinds)
		if err != nil {
			return fmt.Errorf("resolve --owner: %w", err)
		}
		body["owner_id"] = resolvedID
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills/"+url.PathEscape(args[0])+"/forks", body, &result); err != nil {
		return fmt.Errorf("fork skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Skill forked: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	if owner := strVal(result, "owner_id"); owner != "" {
		fmt.Printf("  owner:    %s\n", owner)
	}
	if v := strVal(result, "current_version"); v != "" {
		fmt.Printf("  version:  %s\n", v)
	}
	return nil
}

