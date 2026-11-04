package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica company` reads and sets the company's controls (spec 4.5): pause,
// resume and the daily quiet window. Owners and admins may change them; the
// server decides, the CLI only sends the request.
var companyCmd = &cobra.Command{
	Use:   "company",
	Short: "Show and set the company's controls: status, pause, resume, quiet hours",
}

var companyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the company's state and quiet hours",
	Args:  cobra.NoArgs,
	RunE:  runCompanyStatus,
}

var companyPauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause the company: routine and delegated runs are held (owners and admins)",
	Long: `Pause the company. Routine and delegated runs do not start; chat and runs a
person starts keep working. --stop-running also cancels the routine and delegated
runs already in progress.`,
	Args: cobra.NoArgs,
	RunE: runCompanyPause,
}

var companyResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume the company after a pause (owners and admins)",
	Args:  cobra.NoArgs,
	RunE:  runCompanyResume,
}

var companyQuietCmd = &cobra.Command{
	Use:   "quiet",
	Short: "Show or set the daily quiet window (owners and admins)",
	Long: `Show or set the daily window in which routine runs are deferred, not dropped.
Without flags it prints the window. --enable or --disable switches it, and
--start, --end and --timezone change it. Times are HH:MM in the timezone.

  multica company quiet --enable --start 22:00 --end 07:00 --timezone Europe/Paris`,
	Args: cobra.NoArgs,
	RunE: runCompanyQuiet,
}

var companyExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export the company's structure as JSON (owners and admins)",
	Long: `Export floors, seats, squads with charters and routines, agents' roles and
instructions, pipelines, knowledge kinds, floor chiefs and the governance sections.
Secrets, runtimes, documents and history are not exported; the document says so.

  multica company export --file company.json`,
	Args: cobra.NoArgs,
	RunE: runCompanyExport,
}

var companyImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Recreate a company export in this workspace (owners and admins)",
	Long: `Recreate an export. Entities are matched by name, so importing the same file
twice changes nothing. Agents are created on the runtime given with --runtime;
without it they are reported as blocked. --dry-run prints the plan and writes nothing.

  multica company import --file company.json --runtime <runtime-id> --dry-run`,
	Args: cobra.NoArgs,
	RunE: runCompanyImport,
}

func init() {
	companyExportCmd.Flags().String("file", "", "Write the export to this file instead of stdout")
	companyImportCmd.Flags().String("file", "", "The export to import; required")
	companyImportCmd.Flags().String("runtime", "", "Runtime id the created agents run on")
	companyImportCmd.Flags().Bool("dry-run", false, "Print the plan and write nothing")
	companyImportCmd.Flags().String("output", "table", "Output format: table or json")
	companyStatusCmd.Flags().String("output", "table", "Output format: table or json")
	companyPauseCmd.Flags().Bool("stop-running", false, "Also cancel the routine and delegated runs in progress")
	companyPauseCmd.Flags().String("output", "table", "Output format: table or json")
	companyResumeCmd.Flags().String("output", "table", "Output format: table or json")
	companyQuietCmd.Flags().Bool("enable", false, "Turn the quiet window on")
	companyQuietCmd.Flags().Bool("disable", false, "Turn the quiet window off")
	companyQuietCmd.Flags().String("start", "", "Start of the window, HH:MM")
	companyQuietCmd.Flags().String("end", "", "End of the window, HH:MM")
	companyQuietCmd.Flags().String("timezone", "", "IANA timezone of the window, for example Europe/Paris")
	companyQuietCmd.Flags().String("output", "table", "Output format: table or json")

	companyCmd.AddCommand(companyStatusCmd)
	companyCmd.AddCommand(companyPauseCmd)
	companyCmd.AddCommand(companyResumeCmd)
	companyCmd.AddCommand(companyQuietCmd)
	companyCmd.AddCommand(companyExportCmd)
	companyCmd.AddCommand(companyImportCmd)
}

func runCompanyExport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var doc json.RawMessage
	if err := client.GetJSON(ctx, "/api/office/export", &doc); err != nil {
		return fmt.Errorf("export company: %w", err)
	}
	pretty, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("format export: %w", err)
	}
	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		_, err := fmt.Fprintln(os.Stdout, string(pretty))
		return err
	}
	if err := os.WriteFile(file, append(pretty, '\n'), 0o600); err != nil {
		return fmt.Errorf("write export: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Exported the company to %s\n", file)
	return nil
}

func runCompanyImport(cmd *cobra.Command, _ []string) error {
	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		return fmt.Errorf("--file is required: the export to import")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read --file: %w", err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("--file is not JSON")
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	runtimeID, _ := cmd.Flags().GetString("runtime")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	path := "/api/office/import"
	if dryRun {
		path += "?dry_run=true"
	}
	body := map[string]any{"document": json.RawMessage(data), "runtime_id": runtimeID}
	var report map[string]any
	if err := client.PostJSON(ctx, path, body, &report); err != nil {
		return fmt.Errorf("import company: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("format report: %w", err)
		}
		_, err = fmt.Fprintln(os.Stdout, string(raw))
		return err
	}
	return printImportReport(report)
}

// printImportReport prints the plan line by line, then the counts.
func printImportReport(report map[string]any) error {
	if dry, _ := report["dry_run"].(bool); dry {
		fmt.Fprintln(os.Stdout, "Dry run: nothing was written.")
	}
	items, _ := report["items"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		line := fmt.Sprintf("%-8s %-14s %s", item["action"], item["kind"], item["name"])
		if reason, _ := item["reason"].(string); reason != "" {
			line += " (" + reason + ")"
		}
		fmt.Fprintln(os.Stdout, line)
	}
	fmt.Fprintf(os.Stdout, "created %v, existing %v, blocked %v, skipped %v\n",
		report["created"], report["existing"], report["blocked"], report["skipped"])
	return nil
}

func runCompanyStatus(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var state map[string]any
	if err := client.GetJSON(ctx, "/api/office/company-state", &state); err != nil {
		return fmt.Errorf("read company state: %w", err)
	}
	return printCompanyState(cmd, state)
}

func runCompanyPause(cmd *cobra.Command, _ []string) error {
	stop, _ := cmd.Flags().GetBool("stop-running")
	return putCompanyState(cmd, map[string]any{"state": "paused", "stop_running": stop})
}

func runCompanyResume(cmd *cobra.Command, _ []string) error {
	return putCompanyState(cmd, map[string]any{"state": "running", "stop_running": false})
}

func putCompanyState(cmd *cobra.Command, body map[string]any) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var state map[string]any
	if err := client.PutJSON(ctx, "/api/office/company-state", body, &state); err != nil {
		return fmt.Errorf("change company state: %w", err)
	}
	return printCompanyState(cmd, state)
}

func runCompanyQuiet(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var state map[string]any
	if err := client.GetJSON(ctx, "/api/office/company-state", &state); err != nil {
		return fmt.Errorf("read company state: %w", err)
	}
	change, changing, err := quietChangeFromFlags(cmd)
	if err != nil {
		return err
	}
	if !changing {
		return printCompanyState(cmd, state)
	}
	qh, _ := state["quiet_hours"].(map[string]any)
	var saved map[string]any
	if err := client.PutJSON(ctx, "/api/office/quiet-hours", quietBodyFrom(qh, change), &saved); err != nil {
		return fmt.Errorf("save quiet hours: %w", err)
	}
	return printCompanyState(cmd, saved)
}

// quietChange is what the quiet flags ask for. A nil field keeps the window's value.
type quietChange struct {
	Enable, Disable bool
	Start, End      *string
	Timezone        *string
}

// quietChangeFromFlags reads the quiet flags. changing is false when none was given.
func quietChangeFromFlags(cmd *cobra.Command) (change quietChange, changing bool, err error) {
	enable, _ := cmd.Flags().GetBool("enable")
	disable, _ := cmd.Flags().GetBool("disable")
	if enable && disable {
		return quietChange{}, false, fmt.Errorf("--enable and --disable cannot be combined")
	}
	change.Enable, change.Disable = enable, disable
	if cmd.Flags().Changed("start") {
		v, _ := cmd.Flags().GetString("start")
		change.Start = &v
	}
	if cmd.Flags().Changed("end") {
		v, _ := cmd.Flags().GetString("end")
		change.End = &v
	}
	if cmd.Flags().Changed("timezone") {
		v, _ := cmd.Flags().GetString("timezone")
		change.Timezone = &v
	}
	changing = enable || disable || change.Start != nil || change.End != nil || change.Timezone != nil
	return change, changing, nil
}

// quietBodyFrom is the window the server saves: the current window with the
// requested changes applied.
func quietBodyFrom(current map[string]any, c quietChange) map[string]any {
	body := map[string]any{
		"enabled":  current["enabled"] == true,
		"start":    strVal(current, "start"),
		"end":      strVal(current, "end"),
		"timezone": strVal(current, "timezone"),
	}
	if c.Enable {
		body["enabled"] = true
	}
	if c.Disable {
		body["enabled"] = false
	}
	if c.Start != nil {
		body["start"] = *c.Start
	}
	if c.End != nil {
		body["end"] = *c.End
	}
	if c.Timezone != nil {
		body["timezone"] = *c.Timezone
	}
	return body
}

// printCompanyState prints the state as JSON or as a short table.
func printCompanyState(cmd *cobra.Command, state map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, state)
	}
	qh, _ := state["quiet_hours"].(map[string]any)
	window := "off"
	if qh["enabled"] == true {
		window = "on"
	}
	fmt.Printf("State:       %s (setting: %s)\n", strVal(state, "state"), strVal(state, "setting"))
	fmt.Printf("Quiet hours: %s, %s to %s %s\n", window, strVal(qh, "start"), strVal(qh, "end"), strVal(qh, "timezone"))
	fmt.Printf("Updated:     %s\n", strVal(state, "updated_at"))
	return nil
}
