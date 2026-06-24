package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/recall"
)

var recallCmd = &cobra.Command{
	Use:   "recall",
	Short: "Diagnose and maintain shared-memory recall",
}

func init() {
	recallCmd.AddCommand(newRecallQueryCommand())
	recallCmd.AddCommand(newRecallIndexCommand())
}

func newRecallQueryCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "query",
		Short: "Run the same bounded recall used before agent tasks",
		RunE:  runRecallQuery,
	}
	flags := command.Flags()
	flags.String("vault", "", "Canonical Ars Contexta vault root (env: MULTICA_SHARED_MEMORY_VAULT)")
	flags.String("issue", "", "Issue ID or identifier used to load title and description")
	flags.String("title", "", "Query title when --issue is omitted or needs an override")
	flags.String("description", "", "Query description when --issue is omitted or needs an override")
	flags.String("comment", "", "Current triggering comment text")
	flags.Int("max-hits", 5, "Maximum number of notes (hard cap: 5)")
	flags.Int("budget", 12*1024, "Maximum rendered bundle size in bytes")
	flags.Duration("index-max-age", 7*24*time.Hour, "Maximum accepted index age")
	flags.String("output", "json", "Output format: json")
	return command
}

func newRecallIndexCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "index",
		Short: "Atomically rebuild the canonical notes recall index",
		RunE:  runRecallIndex,
	}
	command.Flags().String("vault", "", "Canonical Ars Contexta vault root (env: MULTICA_SHARED_MEMORY_VAULT)")
	command.Flags().String("output", "json", "Output format: json")
	return command
}

func runRecallQuery(command *cobra.Command, _ []string) error {
	vault, _ := command.Flags().GetString("vault")
	if strings.TrimSpace(vault) == "" {
		vault = strings.TrimSpace(os.Getenv("MULTICA_SHARED_MEMORY_VAULT"))
	}
	issueTitle, _ := command.Flags().GetString("title")
	issueDescription, _ := command.Flags().GetString("description")
	issueReference, _ := command.Flags().GetString("issue")
	if issueReference != "" {
		loadedTitle, loadedDescription, err := loadRecallIssue(command, issueReference)
		if err != nil {
			return err
		}
		if issueTitle == "" {
			issueTitle = loadedTitle
		}
		if issueDescription == "" {
			issueDescription = loadedDescription
		}
	}
	comment, _ := command.Flags().GetString("comment")
	maxHits, _ := command.Flags().GetInt("max-hits")
	budget, _ := command.Flags().GetInt("budget")
	indexMaxAge, _ := command.Flags().GetDuration("index-max-age")
	output, _ := command.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("unsupported output format %q (expected json)", output)
	}

	result := recall.Run(command.Context(), recall.Options{
		VaultRoot: vault, MaxHits: maxHits, MaxBundleBytes: budget, MaxIndexAge: indexMaxAge,
	}, recall.Query{
		IssueTitle: issueTitle, IssueDescription: issueDescription, TriggerComment: comment,
	})
	fmt.Fprintln(os.Stdout, result.Render())
	return nil
}

func loadRecallIssue(command *cobra.Command, issueReference string) (string, string, error) {
	client, err := newAPIClient(command)
	if err != nil {
		return "", "", err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	resolved, err := resolveIssueRef(ctx, client, issueReference)
	if err != nil {
		return "", "", fmt.Errorf("resolve recall issue: %w", err)
	}
	var issue struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+resolved.ID, &issue); err != nil {
		return "", "", fmt.Errorf("get recall issue: %w", err)
	}
	return issue.Title, issue.Description, nil
}

func runRecallIndex(command *cobra.Command, _ []string) error {
	vault, _ := command.Flags().GetString("vault")
	if strings.TrimSpace(vault) == "" {
		vault = strings.TrimSpace(os.Getenv("MULTICA_SHARED_MEMORY_VAULT"))
	}
	output, _ := command.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("unsupported output format %q (expected json)", output)
	}
	index, err := recall.BuildIndex(command.Context(), recall.IndexOptions{VaultRoot: vault})
	if err != nil {
		return err
	}
	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("encode recall index result: %w", err)
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
