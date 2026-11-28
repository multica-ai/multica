// issue-sync mirrors issues between a Multica workspace and a GitLab project.
//
// It is an operator command, not a server feature: it runs against a live
// deployment through the same authenticated API a person uses, so it needs no
// database access and no migration. The transport-independent model and the
// decision rules live in internal/integrations/issuesync; this file only wires
// flags, credentials and the report.
//
// Run it with --dry-run first. A first run against an existing board reports
// every unpaired issue, and creating counterparts for all of them is a decision
// an operator should make after reading that list, not before.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/multica-ai/multica/server/internal/integrations/issuesync"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "issue-sync: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	multicaURL := flag.String("multica-url", os.Getenv("MULTICA_URL"), "Multica base URL (env MULTICA_URL)")
	multicaToken := flag.String("multica-token", os.Getenv("MULTICA_TOKEN"), "Multica API token (env MULTICA_TOKEN)")
	workspaceID := flag.String("workspace-id", os.Getenv("MULTICA_WORKSPACE_ID"), "Multica workspace id (env MULTICA_WORKSPACE_ID)")
	projectID := flag.String("project-id", "", "limit the Multica side to one project id (default: whole workspace)")

	gitlabURL := flag.String("gitlab-url", os.Getenv("GITLAB_URL"), "GitLab base URL (env GITLAB_URL)")
	gitlabToken := flag.String("gitlab-token", os.Getenv("GITLAB_TOKEN"), "GitLab API token (env GITLAB_TOKEN)")
	gitlabProject := flag.Int64("gitlab-project-id", 0, "GitLab numeric project id (required)")

	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	createMissing := flag.Bool("create-missing", false, "create the counterpart of an unpaired issue")
	flag.Parse()

	switch {
	case *multicaURL == "":
		return fmt.Errorf("--multica-url or MULTICA_URL is required")
	case *multicaToken == "":
		return fmt.Errorf("--multica-token or MULTICA_TOKEN is required")
	case *workspaceID == "":
		return fmt.Errorf("--workspace-id or MULTICA_WORKSPACE_ID is required")
	case *gitlabURL == "":
		return fmt.Errorf("--gitlab-url or GITLAB_URL is required")
	case *gitlabToken == "":
		return fmt.Errorf("--gitlab-token or GITLAB_TOKEN is required")
	case *gitlabProject <= 0:
		return fmt.Errorf("--gitlab-project-id is required and must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	multica := issuesync.NewMulticaClient(*multicaURL, *multicaToken, *workspaceID)
	tracker := issuesync.NewGitLabClient(*gitlabURL, *gitlabToken, *gitlabProject)

	report, err := issuesync.Run(ctx, multica, tracker, issuesync.Options{
		ProjectID:        *projectID,
		TrackerProjectID: *gitlabProject,
		DryRun:           *dryRun,
		CreateMissing:    *createMissing,
		Out:              os.Stdout,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\nupdated=%d created=%d conflicts=%d unpaired=%d errors=%d\n",
		report.Updated, report.Created, len(report.Conflicts), len(report.Unpaired), len(report.Errors))
	if *dryRun {
		fmt.Fprintln(os.Stdout, "dry run: nothing was written")
	}
	// A run that hit per-pair errors still reports what it did, but exits
	// non-zero so a scheduled invocation surfaces the failure instead of
	// looking like a clean pass.
	if len(report.Errors) > 0 {
		return fmt.Errorf("%d issue(s) failed to sync", len(report.Errors))
	}
	return nil
}
