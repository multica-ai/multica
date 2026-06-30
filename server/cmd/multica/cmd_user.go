package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// User namespace exists so the daemon-injected `## Requesting User` brief
// has a CLI surface a human can mirror without having to construct
// PATCH /api/me by hand. Today only profile-description is wired; future
// per-user knobs (e.g. preferred language) should land as further
// subcommands here rather than expand the verb surface elsewhere.

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Work with your user account",
}

var userProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Get or update your personal profile",
	Long: "Manage the personal profile that agents see when they pick up a task " +
		"on your behalf. The description is injected into the agent brief under " +
		"`## Requesting User`, so use it to share role, stack, and collaboration " +
		"preferences.",
}

var userProfileGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show your current user profile",
	RunE:  runUserProfileGet,
}

var userProfileUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update your user profile (currently: profile description)",
	Long: "Set the personal profile description that gets injected into agent " +
		"briefs as `## Requesting User`. Pass an empty value to clear it.\n\n" +
		"Pick the input mode that preserves your content:\n" +
		"  --description \"...\"          inline (decodes \\n / \\t escapes)\n" +
		"  --description-stdin           pipe a HEREDOC (preserves verbatim)\n" +
		"  --description-file <path>     read a UTF-8 file (Windows-safe)\n",
	RunE: runUserProfileUpdate,
}

func init() {
	userCmd.AddCommand(userProfileCmd)
	userProfileCmd.AddCommand(userProfileGetCmd)
	userProfileCmd.AddCommand(userProfileUpdateCmd)

	userProfileGetCmd.Flags().String("output", "table", "Output format: table or json")

	userProfileUpdateCmd.Flags().String("description", "", "New profile description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	userProfileUpdateCmd.Flags().Bool("description-stdin", false, "Read description from stdin (preserves multi-line content verbatim)")
	userProfileUpdateCmd.Flags().String("description-file", "", "Read description from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes)")
	userProfileUpdateCmd.Flags().Bool("clear", false, "Clear the profile description (equivalent to --description \"\")")
	userProfileUpdateCmd.Flags().String("output", "table", "Output format: table or json")
}

func runUserProfileGet(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me map[string]any
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return fmt.Errorf("get user profile: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, me)
	}

	printUserProfileTable(os.Stdout, me)
	return nil
}

func runUserProfileUpdate(cmd *cobra.Command, _ []string) error {
	// `--clear` is its own flag (not "pass an empty string") because cobra's
	// default value for a Changed("") flag would otherwise be ambiguous with
	// "user typed `--description ""`". Keep both forms supported — the inline
	// empty string is what someone scripting bash would reach for.
	clearFlag, _ := cmd.Flags().GetBool("clear")
	desc, hasDesc, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}

	if clearFlag && hasDesc {
		return fmt.Errorf("--clear cannot be combined with --description / --description-stdin / --description-file")
	}
	if !clearFlag && !hasDesc && !cmd.Flags().Changed("description") {
		return fmt.Errorf("nothing to update; pass --description, --description-stdin, --description-file, or --clear")
	}

	if clearFlag {
		desc = ""
	}

	body := map[string]any{"profile_description": desc}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var me map[string]any
	if err := client.PatchJSON(ctx, "/api/me", body, &me); err != nil {
		return fmt.Errorf("update user profile: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, me)
	}

	printUserProfileTable(os.Stdout, me)
	return nil
}

func printUserProfileTable(out *os.File, me map[string]any) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "ID\t%s\n", strVal(me, "id"))
	fmt.Fprintf(w, "NAME\t%s\n", strVal(me, "name"))
	fmt.Fprintf(w, "EMAIL\t%s\n", strVal(me, "email"))
	desc := strVal(me, "profile_description")
	if desc == "" {
		desc = "(not set)"
	}
	fmt.Fprintf(w, "PROFILE DESCRIPTION\t%s\n", desc)
}
