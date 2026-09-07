package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectNoteCmd = &cobra.Command{
	Use:   "note",
	Short: "Manage markdown notepads attached to a project",
	Long: "Read and write a project's markdown notepads. Notes are free-form " +
		"scratch space owned by Multica — a running journal, a conclusions " +
		"page, working notes. They are NOT injected into the agent runtime " +
		"brief: list them and read only what the task needs.",
}

var projectNoteListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List a project's notepads (titles only, no bodies)",
	Args:  exactArgs(1),
	RunE:  runProjectNoteList,
}

var projectNoteGetCmd = &cobra.Command{
	Use:   "get <project-id> <note-id>",
	Short: "Print a notepad's full markdown body",
	Args:  exactArgs(2),
	RunE:  runProjectNoteGet,
}

var projectNoteCreateCmd = &cobra.Command{
	Use:   "create <project-id>",
	Short: "Create a notepad (--title required, --body optional)",
	Example: `  # An empty notepad to fill in later
  $ multica project note create PROJ --title "Release journal"

  # Seed it from a local file
  $ multica project note create PROJ --title Findings --body @findings.md

  # Or from stdin
  $ cat notes.md | multica project note create PROJ --title Notes --body -`,
	Args: exactArgs(1),
	RunE: runProjectNoteCreate,
}

var projectNoteAppendCmd = &cobra.Command{
	Use:   "append <project-id> <note-id>",
	Short: "Append markdown to a notepad, keeping existing content",
	Long: "Append to a notepad's body. This is the command to use for a " +
		"journal: the concatenation happens server-side in SQL, so two " +
		"concurrent appends both survive. Prefer this over `update`, which " +
		"replaces the whole body.",
	Example: `  # Add today's entry
  $ multica project note append PROJ note123 --body "## 2026-08-07
Shipped the migration."

  # From a file
  $ multica project note append PROJ note123 --body @entry.md`,
	Args: exactArgs(2),
	RunE: runProjectNoteAppend,
}

var projectNoteUpdateCmd = &cobra.Command{
	Use:   "update <project-id> <note-id>",
	Short: "Replace a notepad's entire body (destructive — prefer append)",
	Long: "Overwrite a notepad's body wholesale. Everything currently in the " +
		"note is lost. For adding to a journal use `append` instead.",
	Args: exactArgs(2),
	RunE: runProjectNoteUpdate,
}

var projectNoteDeleteCmd = &cobra.Command{
	Use:   "delete <project-id> <note-id>",
	Short: "Delete a notepad",
	Args:  exactArgs(2),
	RunE:  runProjectNoteDelete,
}

func init() {
	projectCmd.AddCommand(projectNoteCmd)

	projectNoteCmd.AddCommand(projectNoteListCmd)
	projectNoteCmd.AddCommand(projectNoteGetCmd)
	projectNoteCmd.AddCommand(projectNoteCreateCmd)
	projectNoteCmd.AddCommand(projectNoteAppendCmd)
	projectNoteCmd.AddCommand(projectNoteUpdateCmd)
	projectNoteCmd.AddCommand(projectNoteDeleteCmd)

	projectNoteListCmd.Flags().String("output", "table", "Output format: table or json")
	projectNoteListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// `get` defaults to raw markdown: an agent piping this into its context
	// wants the document, not a JSON envelope it has to unwrap.
	projectNoteGetCmd.Flags().String("output", "raw", "Output format: raw (markdown body only) or json")

	projectNoteCreateCmd.Flags().String("title", "", "Notepad title (required)")
	projectNoteCreateCmd.Flags().String("body", "", "Markdown body; @path reads a file, - reads stdin")
	projectNoteCreateCmd.Flags().String("output", "json", "Output format: table or json")

	projectNoteAppendCmd.Flags().String("body", "", "Markdown to append; @path reads a file, - reads stdin")
	projectNoteAppendCmd.Flags().String("output", "json", "Output format: table or json")

	projectNoteUpdateCmd.Flags().String("title", "", "Also rename the notepad")
	projectNoteUpdateCmd.Flags().String("body", "", "Replacement markdown; @path reads a file, - reads stdin")
	projectNoteUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	projectNoteDeleteCmd.Flags().String("output", "table", "Output format: table or json")
}

// readBodyArg resolves a --body value: `@path` reads that file, `-` reads
// stdin, anything else is the literal text. File and stdin forms exist because
// a markdown body is multi-line and shell-quoting one is error-prone; a
// literal `@` can be escaped as `@@`.
func readBodyArg(raw string) (string, error) {
	switch {
	case raw == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read body from stdin: %w", err)
		}
		return string(data), nil
	case strings.HasPrefix(raw, "@@"):
		return raw[1:], nil
	case strings.HasPrefix(raw, "@"):
		path := raw[1:]
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read body from %s: %w", path, err)
		}
		return string(data), nil
	default:
		return raw, nil
	}
}

// noteTableRow renders one summary row. Bodies are absent from list responses,
// so the size column comes from the server-computed body_size.
func noteTableRow(n map[string]any, fullID bool) []string {
	return []string{
		displayID(strVal(n, "id"), fullID),
		strVal(n, "title"),
		noteSizeCell(n["body_size"]),
		strVal(n, "updated_at"),
	}
}

// noteSizeCell renders the `body_size` field of a note-summary response.
//
// It cannot go through strVal: JSON numbers decode as float64, and strVal's
// %v prints a 1.2MB note as "1.234567e+06". formatBytes is the same renderer
// the daemon and skill tables use.
func noteSizeCell(v any) string {
	size, ok := v.(float64)
	if !ok {
		return "-"
	}
	return formatBytes(int64(size))
}

// resolveNoteTarget resolves the project reference and returns the note path
// prefix shared by every per-note endpoint.
func resolveNoteTarget(ctx context.Context, client *cli.APIClient, projectArg string) (string, error) {
	projectRef, err := resolveProjectID(ctx, client, projectArg)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	return "/api/projects/" + url.PathEscape(projectRef.ID) + "/notes", nil
}

// resolveNotePath resolves both the project and the note, returning the full
// path to that note. The note argument accepts the truncated id that
// `note list` prints as well as a full UUID — the server only accepts a UUID,
// so without this resolution step the ids shown by `list` would be unusable.
func resolveNotePath(ctx context.Context, client *cli.APIClient, projectArg, noteArg string) (string, error) {
	projectRef, err := resolveProjectID(ctx, client, projectArg)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	noteRef, err := resolveProjectNoteID(ctx, client, projectRef.ID, noteArg)
	if err != nil {
		return "", fmt.Errorf("resolve note: %w", err)
	}
	return "/api/projects/" + url.PathEscape(projectRef.ID) + "/notes/" + url.PathEscape(noteRef.ID), nil
}

func runProjectNoteList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	base, err := resolveNoteTarget(ctx, client, args[0])
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.GetJSON(ctx, base, &result); err != nil {
		return fmt.Errorf("list project notes: %w", err)
	}
	notesRaw, _ := result["notes"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, notesRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "SIZE", "UPDATED"}
	rows := make([][]string, 0, len(notesRaw))
	for _, raw := range notesRaw {
		n, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, noteTableRow(n, fullID))
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectNoteGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path, err := resolveNotePath(ctx, client, args[0], args[1])
	if err != nil {
		return err
	}

	var note map[string]any
	if err := client.GetJSON(ctx, path, &note); err != nil {
		return fmt.Errorf("get project note: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, note)
	}
	fmt.Fprintln(os.Stdout, strVal(note, "body_md"))
	return nil
}

func runProjectNoteCreate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}

	payload := map[string]any{"title": title}
	if rawBody, _ := cmd.Flags().GetString("body"); rawBody != "" {
		body, err := readBodyArg(rawBody)
		if err != nil {
			return err
		}
		payload["body_md"] = body
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	base, err := resolveNoteTarget(ctx, client, args[0])
	if err != nil {
		return err
	}

	var note map[string]any
	if err := client.PostJSON(ctx, base, payload, &note); err != nil {
		return fmt.Errorf("create project note: %w", err)
	}
	return printNoteResult(cmd, note)
}

func runProjectNoteAppend(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	rawBody, _ := cmd.Flags().GetString("body")
	if rawBody == "" {
		return fmt.Errorf("--body is required")
	}
	body, err := readBodyArg(rawBody)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("--body resolved to empty content")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path, err := resolveNotePath(ctx, client, args[0], args[1])
	if err != nil {
		return err
	}

	var note map[string]any
	if err := client.PostJSON(ctx, path+"/append", map[string]any{"body_md": body}, &note); err != nil {
		return fmt.Errorf("append to project note: %w", err)
	}
	return printNoteResult(cmd, note)
}

func runProjectNoteUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	payload := map[string]any{}
	if rawBody, _ := cmd.Flags().GetString("body"); rawBody != "" {
		body, err := readBodyArg(rawBody)
		if err != nil {
			return err
		}
		payload["body_md"] = body
	}
	if title, _ := cmd.Flags().GetString("title"); strings.TrimSpace(title) != "" {
		payload["title"] = title
	}
	if len(payload) == 0 {
		return fmt.Errorf("nothing to update: pass --body and/or --title")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path, err := resolveNotePath(ctx, client, args[0], args[1])
	if err != nil {
		return err
	}

	var note map[string]any
	if err := client.PatchJSON(ctx, path, payload, &note); err != nil {
		return fmt.Errorf("update project note: %w", err)
	}
	return printNoteResult(cmd, note)
}

func runProjectNoteDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path, err := resolveNotePath(ctx, client, args[0], args[1])
	if err != nil {
		return err
	}

	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete project note: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Note %s deleted.\n", args[1])
	return nil
}

// printNoteResult renders a single note after a write. Table output stays terse
// — the body just went over the wire from the caller, so echoing it adds noise.
func printNoteResult(cmd *cobra.Command, note map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, note)
	}
	body := strVal(note, "body_md")
	headers := []string{"ID", "TITLE", "SIZE"}
	rows := [][]string{{
		strVal(note, "id"),
		strVal(note, "title"),
		formatBytes(int64(len(body))),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
