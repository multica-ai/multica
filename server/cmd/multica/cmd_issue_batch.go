package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue batch update — one change applied to many issues through
// POST /api/issues/batch-update. Each issue is written on its own, so the
// server answers with a result per issue: updated, unchanged, or skipped with
// a reason. Deletion in batch is `multica issue delete <id>...`.

const issueBatchChunkSize = 100

var issueBatchCmd = &cobra.Command{
	Use:   "batch",
	Short: "Change many issues at once",
}

var issueBatchUpdateCmd = &cobra.Command{
	Use:   "update <id>...",
	Short: "Apply the same change to several issues",
	Long: "Apply the same change to several issues.\n\n" +
		"Ids may be issue keys (e.g. MUL-123) or full UUIDs, as arguments or one per line\n" +
		"with --ids-stdin. Every issue is written on its own: the command prints one row per\n" +
		"issue saying whether it was updated, already matched (unchanged) or skipped and why.\n" +
		"It exits non-zero when any issue was skipped, after printing the rows.\n\n" +
		"Labels: --add-label and --remove-label leave other labels alone; --clear-labels\n" +
		"removes every label first, so --clear-labels --add-label X replaces the set.\n" +
		"Custom properties: --property Name=Value sets a value (same forms as\n" +
		"`issue create --property`); --unset-property Name clears it.\n\n" +
		"--dry-run runs every check and prints the same rows without writing anything.",
	RunE: runIssueBatchUpdate,
}

func init() {
	issueBatchCmd.AddCommand(issueBatchUpdateCmd)
	issueCmd.AddCommand(issueBatchCmd)
	addIssueBatchUpdateFlags(issueBatchUpdateCmd)
}

func addIssueBatchUpdateFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Bool("ids-stdin", false, "Read issue ids from stdin, one per line (in addition to arguments)")
	f.String("status", "", "New status")
	f.String("priority", "", "New priority")
	f.String("assignee", "", "New assignee name (member, agent, or squad; fuzzy match)")
	f.String("assignee-id", "", "New assignee UUID — member, agent, or squad (mutually exclusive with --assignee)")
	f.Bool("unassign", false, "Remove the assignee")
	f.String("project", "", "Project ID (use --project \"\" to clear)")
	f.String("start-date", "", "Start date (YYYY-MM-DD; pass empty string to clear)")
	f.String("due-date", "", "Due date (YYYY-MM-DD; pass empty string to clear)")
	f.String("parent", "", "Parent issue ID (use --parent \"\" to clear)")
	f.Int("stage", 0, "Stage ordinal (>=1) for sub-issues; see `issue create --stage`")
	f.StringArray("add-label", nil, "Label name or UUID to add (repeatable)")
	f.StringArray("remove-label", nil, "Label name or UUID to remove (repeatable)")
	f.Bool("clear-labels", false, "Remove every label before --add-label is applied")
	f.StringArray("property", nil, `Set a custom property as "Name=Value" (repeatable; same value forms as issue create --property)`)
	f.StringArray("unset-property", nil, "Custom property name or UUID to clear (repeatable)")
	f.Bool("no-start", false, "Apply the change without starting agent runs")
	f.Bool("dry-run", false, "Report what would change without writing")
	f.String("output", "table", "Output format: table or json")
}

type issueBatchResult struct {
	IssueID    string `json:"issue_id"`
	Identifier string `json:"identifier,omitempty"`
	Outcome    string `json:"outcome"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	// RunOutcome is kept so --output json passes it through.
	RunOutcome *issueRunOutcome `json:"run_outcome,omitempty"`
}

type issueBatchResponse struct {
	Updated int                `json:"updated"`
	Deleted int                `json:"deleted"`
	DryRun  bool               `json:"dry_run,omitempty"`
	Results []issueBatchResult `json:"results"`
}

// requireIssueBatchOps fails closed on servers that predate per-issue results:
// they ignore labels, properties and dry_run, so a dry run there would write.
func requireIssueBatchOps(ctx context.Context, client *cli.APIClient) error {
	var config struct {
		Supported bool `json:"issue_batch_ops_supported"`
	}
	if err := client.GetJSON(ctx, "/api/config", &config); err != nil {
		return fmt.Errorf("check batch support: %w", err)
	}
	if !config.Supported {
		return errors.New("this server version does not support batch results or dry runs; update the server, or change issues one at a time with `multica issue update`")
	}
	return nil
}

func collectIssueBatchArgs(cmd *cobra.Command, args []string, stdin io.Reader) ([]string, error) {
	refs := append([]string(nil), args...)
	if fromStdin, _ := cmd.Flags().GetBool("ids-stdin"); fromStdin {
		scanner := bufio.NewScanner(stdin)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" {
				refs = append(refs, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read ids from stdin: %w", err)
		}
	}
	if len(refs) == 0 {
		return nil, errors.New("no issues given; pass ids as arguments or with --ids-stdin")
	}
	return refs, nil
}

func buildIssueBatchUpdateBody(ctx context.Context, client *cli.APIClient, cmd *cobra.Command) (map[string]any, error) {
	flags := cmd.Flags()
	updates := map[string]any{}
	if flags.Changed("status") {
		v, _ := flags.GetString("status")
		if err := validateIssueStatus(v); err != nil {
			return nil, err
		}
		updates["status"] = v
	}
	if flags.Changed("priority") {
		v, _ := flags.GetString("priority")
		if err := validateIssuePriority(v); err != nil {
			return nil, err
		}
		updates["priority"] = v
	}
	unassign, _ := flags.GetBool("unassign")
	if unassign && (flags.Changed("assignee") || flags.Changed("assignee-id")) {
		return nil, errors.New("--unassign cannot be combined with --assignee or --assignee-id")
	}
	if unassign {
		updates["assignee_type"] = nil
		updates["assignee_id"] = nil
	} else if flags.Changed("assignee") || flags.Changed("assignee-id") {
		aType, aID, ok, err := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
		if err != nil {
			return nil, fmt.Errorf("resolve assignee: %w", err)
		}
		if ok {
			updates["assignee_type"] = aType
			updates["assignee_id"] = aID
		}
	}
	if flags.Changed("project") {
		v, _ := flags.GetString("project")
		if v == "" {
			updates["project_id"] = nil
		} else {
			project, err := resolveProjectID(ctx, client, v)
			if err != nil {
				return nil, fmt.Errorf("resolve project: %w", err)
			}
			updates["project_id"] = project.ID
		}
	}
	for _, name := range []string{"start-date", "due-date"} {
		if flags.Changed(name) {
			v, _ := flags.GetString(name)
			updates[strings.ReplaceAll(name, "-", "_")] = v
		}
	}
	if flags.Changed("parent") {
		v, _ := flags.GetString("parent")
		if v == "" {
			updates["parent_issue_id"] = nil
		} else {
			parent, err := resolveIssueRef(ctx, client, v)
			if err != nil {
				return nil, fmt.Errorf("resolve parent issue: %w", err)
			}
			updates["parent_issue_id"] = parent.ID
		}
	}
	if flags.Changed("stage") {
		stage, _ := flags.GetInt("stage")
		if stage < 1 {
			return nil, errors.New("--stage must be >= 1")
		}
		updates["stage"] = stage
	}

	body := map[string]any{}
	if len(updates) > 0 {
		if noStart, _ := flags.GetBool("no-start"); noStart {
			updates["suppress_run"] = true
		}
		body["updates"] = updates
	}

	addRefs, _ := flags.GetStringArray("add-label")
	removeRefs, _ := flags.GetStringArray("remove-label")
	clearLabels, _ := flags.GetBool("clear-labels")
	if len(addRefs) > 0 || len(removeRefs) > 0 || clearLabels {
		candidates, err := fetchLabelCandidates(ctx, client, "issue")
		if err != nil {
			return nil, fmt.Errorf("list labels: %w", err)
		}
		add, err := resolveIssueBatchLabels(ctx, client, candidates, addRefs)
		if err != nil {
			return nil, err
		}
		remove, err := resolveIssueBatchLabels(ctx, client, candidates, removeRefs)
		if err != nil {
			return nil, err
		}
		if clearLabels && len(remove) > 0 {
			return nil, errors.New("--clear-labels already removes every label; drop --remove-label")
		}
		body["labels"] = map[string]any{"add": add, "remove": remove, "clear": clearLabels}
	}

	setPairs, _ := flags.GetStringArray("property")
	unsetRefs, _ := flags.GetStringArray("unset-property")
	if len(setPairs) > 0 || len(unsetRefs) > 0 {
		properties := map[string]any{}
		if len(setPairs) > 0 {
			set, err := buildIssuePropertyValues(ctx, client, setPairs, "in a batch update", "cannot unset a property; use --unset-property")
			if err != nil {
				return nil, err
			}
			properties["set"] = set
		}
		if len(unsetRefs) > 0 {
			catalog, err := fetchProperties(ctx, client)
			if err != nil {
				return nil, err
			}
			unset := make([]string, 0, len(unsetRefs))
			for _, ref := range unsetRefs {
				property, err := resolvePropertyRef(catalog, ref)
				if err != nil {
					return nil, err
				}
				unset = append(unset, property.ID)
			}
			properties["unset"] = unset
		}
		body["properties"] = properties
	}

	if len(body) == 0 {
		return nil, errors.New("nothing to change; use flags like --status, --assignee, --add-label or --property")
	}
	if dryRun, _ := flags.GetBool("dry-run"); dryRun {
		body["dry_run"] = true
	}
	return body, nil
}

// resolveIssueBatchLabels accepts a label's name (case-insensitive) as well
// as its UUID or UUID prefix.
func resolveIssueBatchLabels(ctx context.Context, client *cli.APIClient, candidates []idCandidate, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		var byName []idCandidate
		for _, c := range candidates {
			if strings.EqualFold(c.Display, strings.TrimSpace(ref)) {
				byName = append(byName, c)
			}
		}
		switch {
		case len(byName) == 1:
			ids = append(ids, byName[0].ID)
		case len(byName) > 1:
			return nil, fmt.Errorf("label name %q is ambiguous; pass its UUID", ref)
		default:
			label, err := resolveLabelID(ctx, client, ref, "issue")
			if err != nil {
				return nil, fmt.Errorf("resolve label %q: %w", ref, err)
			}
			ids = append(ids, label.ID)
		}
	}
	return ids, nil
}

func runIssueBatchUpdate(cmd *cobra.Command, args []string) error {
	refs, err := collectIssueBatchArgs(cmd, args, os.Stdin)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	prepCtx, cancelPrep := cli.APIContext(context.Background())
	defer cancelPrep()
	if err := requireIssueBatchOps(prepCtx, client); err != nil {
		return err
	}
	body, err := buildIssueBatchUpdateBody(prepCtx, client, cmd)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		resolved, err := resolveIssueRef(prepCtx, client, ref)
		if err != nil {
			return fmt.Errorf("resolve issue %q: %w", ref, err)
		}
		if _, dup := seen[resolved.ID]; dup {
			continue
		}
		seen[resolved.ID] = struct{}{}
		ids = append(ids, resolved.ID)
	}

	var combined issueBatchResponse
	for start := 0; start < len(ids); start += issueBatchChunkSize {
		end := min(start+issueBatchChunkSize, len(ids))
		body["issue_ids"] = ids[start:end]
		var resp issueBatchResponse
		ctx, cancel := cli.APIContext(context.Background())
		err := client.PostJSON(ctx, "/api/issues/batch-update", body, &resp)
		cancel()
		if err != nil {
			if start > 0 {
				printIssueBatchResults(cmd, combined)
				return fmt.Errorf("batch update stopped after %d of %d issues: %w", start, len(ids), err)
			}
			return fmt.Errorf("batch update: %w", err)
		}
		if resp.Results == nil {
			return errors.New("the server did not return per-issue results; update the server before using `issue batch update`")
		}
		combined.Updated += resp.Updated
		combined.DryRun = resp.DryRun
		combined.Results = append(combined.Results, resp.Results...)
	}
	return finishIssueBatch(cmd, combined)
}

// finishIssueBatch prints the rows and turns any skipped issue into a
// non-zero exit, so scripts cannot mistake a partial batch for success.
func finishIssueBatch(cmd *cobra.Command, resp issueBatchResponse) error {
	if err := printIssueBatchResults(cmd, resp); err != nil {
		return err
	}
	skipped := 0
	for _, r := range resp.Results {
		if r.Outcome == "skipped" {
			skipped++
		}
	}
	if skipped > 0 {
		return fmt.Errorf("%d of %d issues were skipped; see the reasons above", skipped, len(resp.Results))
	}
	return nil
}

func printIssueBatchResults(cmd *cobra.Command, resp issueBatchResponse) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	noStart, _ := cmd.Flags().GetBool("no-start")
	rows := make([][]string, 0, len(resp.Results))
	counts := map[string]int{}
	for _, r := range resp.Results {
		key := r.Identifier
		if key == "" {
			key = r.IssueID
		}
		reason := r.Message
		if r.Code != "" && reason == "" {
			reason = r.Code
		}
		if reason == "" {
			reason = issueRunOutcomeNote(r.RunOutcome, noStart)
		}
		rows = append(rows, []string{key, strings.ReplaceAll(r.Outcome, "_", " "), reason})
		counts[r.Outcome]++
	}
	cli.PrintTable(os.Stdout, []string{"ISSUE", "OUTCOME", "REASON"}, rows)
	var parts []string
	for _, outcome := range []string{"updated", "would_update", "unchanged", "deleted", "would_delete", "skipped"} {
		if counts[outcome] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[outcome], strings.ReplaceAll(outcome, "_", " ")))
		}
	}
	if len(parts) > 0 {
		prefix := ""
		if resp.DryRun {
			prefix = "Dry run, nothing written: "
		}
		fmt.Fprintln(os.Stderr, prefix+strings.Join(parts, ", ")+".")
	}
	return nil
}
