package main

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue delete <id>... — permanent deletion for one issue or a batch.
//
// The server exposes two routes: DELETE /api/issues/{id} for one issue and
// POST /api/issues/batch-delete for a multi-select. The batch route accepts
// UUIDs only and silently skips references it cannot parse, so every argument
// is resolved first and the UUIDs are sent; a typo therefore fails before
// anything is deleted instead of producing a partial batch.
//
// Deletion is irreversible and has side effects the issue page does not make
// obvious: comments and attachment objects go with the issue, in-flight runs
// are cancelled, linked autopilot runs fail, duplicate marks that pointed at
// the issue are cleared, and child issues are detached rather than deleted.
// The confirmation says so.

var issueDeleteCmd = &cobra.Command{
	Use:   "delete <id>...",
	Short: "Delete one or more issues",
	Long: "Delete one or more issues permanently.\n\n" +
		"Deletion removes the issue's comments and attachments, cancels its in-flight\n" +
		"runs, fails linked autopilot runs, clears duplicate marks that pointed at it,\n" +
		"and detaches its child issues (children are not deleted). It cannot be undone.\n\n" +
		"Pass several ids to delete them in one batch. Every id is resolved with a read\n" +
		"before the prompt, full UUIDs included, so an unknown, stale or invisible issue\n" +
		"aborts the whole command instead of leaving a partial batch. Ids may be issue\n" +
		"keys (e.g. MUL-123) or full UUIDs.\n\n" +
		"Without --yes the command asks for confirmation. A non-interactive run that\n" +
		"cannot answer the prompt fails without deleting; pass --yes to skip it.",
	Args: minimumArgs(1),
	RunE: runIssueDelete,
}

func runIssueDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	resolveCtx, cancelResolve := cli.APIContext(context.Background())
	refs, err := resolveIssueDeleteRefs(resolveCtx, client, args)
	cancelResolve()
	if err != nil {
		return err
	}

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Fprint(os.Stderr, issueDeleteConfirmation(refs))
		answer, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			if answer == "" && readErr != nil {
				return fmt.Errorf("no confirmation received (stdin is not interactive); re-run with --yes to delete without a prompt")
			}
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	// Waiting for confirmation must not consume the deletion request's timeout.
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	output, _ := cmd.Flags().GetString("output")

	// One id uses the per-issue route the app uses; several use the batch
	// route so the permission check covers the whole selection before any
	// issue is removed.
	if len(refs) == 1 {
		if err := client.DeleteJSON(ctx, "/api/issues/"+url.PathEscape(refs[0].ID)); err != nil {
			return issueDeleteError("delete issue "+refs[0].Display, err)
		}
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"id": refs[0].ID, "deleted": true})
		}
		fmt.Fprintf(os.Stdout, "Deleted issue %s\n", refs[0].Display)
		return nil
	}

	ids := make([]string, len(refs))
	for i, ref := range refs {
		ids[i] = ref.ID
	}
	var result struct {
		Deleted int `json:"deleted"`
	}
	if err := client.PostJSON(ctx, "/api/issues/batch-delete", map[string]any{"issue_ids": ids}, &result); err != nil {
		return issueDeleteError("delete issues", err)
	}
	if result.Deleted != len(ids) {
		// The batch route skips an id that disappeared or became invisible
		// between resolution and deletion; saying how many landed is more
		// useful than reporting the whole batch as failed, and JSON consumers
		// must not see a success-shaped document.
		if output == "json" {
			if err := cli.PrintJSON(os.Stdout, map[string]any{
				"ids":           ids,
				"deleted":       false,
				"deleted_count": result.Deleted,
			}); err != nil {
				return err
			}
		}
		return fmt.Errorf("server deleted %d of %d issues; the rest were already gone or not visible to this account — run `multica issue list` to check before retrying", result.Deleted, len(ids))
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"ids": ids, "deleted": true})
	}
	for _, ref := range refs {
		fmt.Fprintf(os.Stdout, "Deleted issue %s\n", ref.Display)
	}
	return nil
}

// issueDeleteError keeps the step that failed and the server's own refusal
// text — the 403 copy naming who may delete, or the 404 copy — instead of
// FormatError's generic kind message.
func issueDeleteError(step string, err error) error {
	if msg := cli.ServerMessage(err); msg != "" {
		return cli.WithUserMessage(step+": "+msg, err)
	}
	return fmt.Errorf("%s: %w", step, err)
}

// resolveIssueDeleteRefs resolves and de-duplicates every argument, keeping
// the caller's order. Resolution is a read, so it can happen before the
// confirmation without touching anything.
func resolveIssueDeleteRefs(ctx context.Context, client *cli.APIClient, args []string) ([]resolvedID, error) {
	refs := make([]resolvedID, 0, len(args))
	seen := make(map[string]struct{}, len(args))
	for _, arg := range args {
		ref, err := resolveIssueDeleteRef(ctx, client, arg)
		if err != nil {
			return nil, issueDeleteError(fmt.Sprintf("resolve issue %q", arg), err)
		}
		if _, duplicate := seen[ref.ID]; duplicate {
			continue
		}
		seen[ref.ID] = struct{}{}
		refs = append(refs, ref)
	}
	return refs, nil
}

// resolveIssueDeleteRef resolves one argument with a read. A full UUID goes
// through the same GET as a key: resolveIssueRef trusts a UUID without asking
// the server (GH #7017), but delete cannot — the batch route silently skips a
// stale UUID and deletes the rest, so the failure would land after the writes.
func resolveIssueDeleteRef(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	trimmed := strings.TrimSpace(input)
	if uuidRegexp.MatchString(trimmed) {
		return fetchIssueRef(ctx, client, strings.ToLower(trimmed))
	}
	return resolveIssueRef(ctx, client, trimmed)
}

func issueDeleteConfirmation(refs []resolvedID) string {
	keys := make([]string, len(refs))
	for i, ref := range refs {
		keys[i] = ref.Display
	}

	var target, possessive string
	if len(keys) == 1 {
		target = "issue " + keys[0]
		possessive = "its"
	} else {
		target = fmt.Sprintf("%d issues (%s)", len(keys), strings.Join(keys, ", "))
		possessive = "their"
	}

	return fmt.Sprintf(
		"Delete %s? This permanently removes %s comments and attachments, cancels in-flight runs and detaches child issues; it cannot be undone. [y/N] ",
		target,
		possessive,
	)
}
