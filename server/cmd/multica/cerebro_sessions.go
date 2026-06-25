package main

// CEREBRO-PATCH(cerebro-sessions-cli): FIR-2021 — agent-facing CLI for the
// thread = session model (FIR-1874). A session IS a comment thread: a new
// top-level comment starts a fresh session, a reply resumes it. These commands
// let an agent close a session (resolve its thread root), rename it, hand it
// off, and list the sessions on an issue — the backend endpoints already exist
// (server/internal/cerebro/sessions + the upstream comment resolve route); this
// file only wraps them so agents don't have to call the HTTP API directly.
//
// Registered in init() onto the existing issue/comment command tree, so this is
// a self-contained cerebro-zone sibling file with no upstream-file edits.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// --- issue comment resolve / unresolve --------------------------------------

var issueCommentResolveCmd = &cobra.Command{
	Use:   "resolve <comment-id>",
	Short: "Resolve a comment thread (closes its session)",
	Long: "Resolve a thread by its root comment id. In the thread = session model a " +
		"resolved thread root means the session is closed. Pass the THREAD ROOT comment id.",
	Args: cobra.ExactArgs(1),
	RunE: runIssueCommentResolve,
}

var issueCommentUnresolveCmd = &cobra.Command{
	Use:   "unresolve <comment-id>",
	Short: "Reopen a resolved comment thread (reopens its session)",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCommentUnresolve,
}

func runIssueCommentResolve(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/comments/"+url.PathEscape(args[0])+"/resolve", map[string]any{}, &result); err != nil {
		return fmt.Errorf("resolve comment: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Comment thread %s resolved (session closed).\n", args[0])
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueCommentUnresolve(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/comments/"+url.PathEscape(args[0])+"/resolve"); err != nil {
		return fmt.Errorf("unresolve comment: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Comment thread %s unresolved (session reopened).\n", args[0])
	return nil
}

// --- issue session (list / rename / handoff) --------------------------------

var issueSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Work with issue sessions (a session is a comment thread)",
	Long: "A session IS a comment thread (FIR-1874): the thread root comment id is the " +
		"session id, open/closed is the thread root's resolved state. Start a fresh " +
		"session by posting a new top-level comment (issue comment add without --parent); " +
		"resume one by replying in its thread.",
}

var issueSessionListCmd = &cobra.Command{
	Use:   "list <issue>",
	Short: "List the sessions (named threads) on an issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueSessionList,
}

var issueSessionRenameCmd = &cobra.Command{
	Use:   "rename <issue> <root-comment-id>",
	Short: "Set a session's name (the thread's display name)",
	Args:  cobra.ExactArgs(2),
	RunE:  runIssueSessionRename,
}

var issueSessionHandoffCmd = &cobra.Command{
	Use:   "handoff <issue> <root-comment-id>",
	Short: "Hand off a session: close its thread and store a carry-over brief",
	Long: "Resolve the thread (closing the session) and store a handoff brief on it. " +
		"With no --summary/--done/--remaining the server auto-summarises the thread. " +
		"Start the fresh session afterwards by posting a new top-level comment.",
	Args: cobra.ExactArgs(2),
	RunE: runIssueSessionHandoff,
}

func runIssueSessionList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	var result any
	if err := client.GetJSON(ctx, "/api/cerebro/issues/"+url.PathEscape(issueRef.ID)+"/sessions", &result); err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueSessionRename(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{"name": name}
	var result map[string]any
	path := "/api/cerebro/issues/" + url.PathEscape(issueRef.ID) + "/sessions/" + url.PathEscape(args[1])
	if err := client.PatchJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Session %s renamed to %q.\n", args[1], name)
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueSessionHandoff(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{"root_comment_id": args[1]}
	summary, _ := cmd.Flags().GetString("summary")
	done, _ := cmd.Flags().GetStringSlice("done")
	remaining, _ := cmd.Flags().GetStringSlice("remaining")
	planRef, _ := cmd.Flags().GetString("plan-ref")
	// Only send a handoff object when the caller supplied content; otherwise the
	// server auto-summarises the thread.
	if summary != "" || len(done) > 0 || len(remaining) > 0 || planRef != "" {
		handoff := map[string]any{
			"summary":   summary,
			"done":      done,
			"remaining": remaining,
		}
		if planRef != "" {
			handoff["plan_ref"] = planRef
		}
		body["handoff"] = handoff
	}

	var result map[string]any
	path := "/api/cerebro/issues/" + url.PathEscape(issueRef.ID) + "/sessions/start-fresh"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("hand off session: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Session %s handed off and closed. Post a new top-level comment to start the fresh session.\n", args[1])
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func init() {
	// Comment thread resolve / unresolve.
	issueCommentResolveCmd.Flags().String("output", "json", "Output format: table or json")
	issueCommentCmd.AddCommand(issueCommentResolveCmd)
	issueCommentCmd.AddCommand(issueCommentUnresolveCmd)

	// Session list / rename / handoff.
	issueSessionListCmd.Flags().String("output", "json", "Output format: json")

	issueSessionRenameCmd.Flags().String("name", "", "New session name (required)")
	issueSessionRenameCmd.Flags().String("output", "json", "Output format: table or json")

	issueSessionHandoffCmd.Flags().String("summary", "", "Handoff summary (omit to auto-summarise the thread)")
	issueSessionHandoffCmd.Flags().StringSlice("done", nil, "What was completed (repeatable)")
	issueSessionHandoffCmd.Flags().StringSlice("remaining", nil, "What is left to do (repeatable)")
	issueSessionHandoffCmd.Flags().String("plan-ref", "", "Reference to a plan artifact/issue")
	issueSessionHandoffCmd.Flags().String("output", "json", "Output format: table or json")

	issueSessionCmd.AddCommand(issueSessionListCmd)
	issueSessionCmd.AddCommand(issueSessionRenameCmd)
	issueSessionCmd.AddCommand(issueSessionHandoffCmd)
	issueCmd.AddCommand(issueSessionCmd)
}
