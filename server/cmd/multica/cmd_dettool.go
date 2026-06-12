package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var dettoolCmd = &cobra.Command{
	Use:     "dettool",
	Aliases: []string{"deterministic-tool", "deterministic-tools"},
	Short:   "Work with workspace-authored deterministic tools",
}

var dettoolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List deterministic tools in the workspace",
	RunE:  runDettoolList,
}

var dettoolGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get deterministic tool details",
	Args:  exactArgs(1),
	RunE:  runDettoolGet,
}

var dettoolCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a deterministic tool",
	RunE:  runDettoolCreate,
}

var dettoolUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a deterministic tool",
	Args:  exactArgs(1),
	RunE:  runDettoolUpdate,
}

var dettoolDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a deterministic tool",
	Args:  exactArgs(1),
	RunE:  runDettoolDelete,
}

var dettoolTestCmd = &cobra.Command{
	Use:   "test [id]",
	Short: "Run deterministic tool source through the sandboxed test endpoint",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDettoolTest,
}

var dettoolImportFileCmd = &cobra.Command{
	Use:   "import-file <path>",
	Short: "Create or update a deterministic tool from a Go source file",
	Args:  exactArgs(1),
	RunE:  runDettoolImportFile,
}

func init() {
	dettoolCmd.AddCommand(dettoolListCmd)
	dettoolCmd.AddCommand(dettoolGetCmd)
	dettoolCmd.AddCommand(dettoolCreateCmd)
	dettoolCmd.AddCommand(dettoolUpdateCmd)
	dettoolCmd.AddCommand(dettoolDeleteCmd)
	dettoolCmd.AddCommand(dettoolTestCmd)
	dettoolCmd.AddCommand(dettoolImportFileCmd)

	dettoolListCmd.Flags().String("output", "table", "Output format: table or json")

	dettoolGetCmd.Flags().String("output", "json", "Output format: table or json")

	dettoolCreateCmd.Flags().String("name", "", "Tool name (required, lowercase snake_case)")
	dettoolCreateCmd.Flags().String("description", "", "Tool description")
	addDettoolSourceFlags(dettoolCreateCmd, "Tool source")
	dettoolCreateCmd.Flags().Bool("enabled", true, "Whether the tool is enabled")
	dettoolCreateCmd.Flags().String("output", "json", "Output format: table or json")

	dettoolUpdateCmd.Flags().String("name", "", "New tool name")
	dettoolUpdateCmd.Flags().String("description", "", "New tool description")
	addDettoolSourceFlags(dettoolUpdateCmd, "New tool source")
	dettoolUpdateCmd.Flags().Bool("enabled", true, "Whether the tool is enabled")
	dettoolUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	dettoolDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	addDettoolSourceFlags(dettoolTestCmd, "Tool source")
	dettoolTestCmd.Flags().String("input", "{}", "Sample input JSON object")
	dettoolTestCmd.Flags().String("input-file", "", "Read sample input JSON object from a file")
	dettoolTestCmd.Flags().String("output", "json", "Output format: json or table")

	dettoolImportFileCmd.Flags().String("name", "", "Tool name (defaults to the source file stem)")
	dettoolImportFileCmd.Flags().String("description", "", "Tool description")
	dettoolImportFileCmd.Flags().Bool("enabled", true, "Whether the tool is enabled on create, or when --enabled is explicitly set on update")
	dettoolImportFileCmd.Flags().Bool("update-existing", true, "Update the existing tool with the same name instead of failing")
	dettoolImportFileCmd.Flags().String("output", "json", "Output format: table or json")
}

func addDettoolSourceFlags(cmd *cobra.Command, label string) {
	cmd.Flags().String("source", "", label)
	cmd.Flags().Bool("source-stdin", false, "Read tool source from stdin. Mutually exclusive with --source and --source-file.")
	cmd.Flags().String("source-file", "", "Read tool source from a UTF-8 file. Mutually exclusive with --source and --source-stdin.")
}

func resolveDettoolSourceFlag(cmd *cobra.Command) (string, bool, error) {
	useStdin, _ := cmd.Flags().GetBool("source-stdin")
	inline, _ := cmd.Flags().GetString("source")
	filePath, _ := cmd.Flags().GetString("source-file")
	inlineSet := cmd.Flags().Changed("source")

	sources := 0
	if inlineSet {
		sources++
	}
	if useStdin {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if sources > 1 {
		return "", false, fmt.Errorf("--source, --source-stdin, and --source-file are mutually exclusive")
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --source-stdin: %w", err)
		}
		return dettoolSourceBytesToString(data, "stdin source for --source-stdin")
	}
	if filePath != "" {
		return readDettoolSourceFile(filePath)
	}
	if inlineSet {
		return inline, true, nil
	}
	return "", false, nil
}

func readDettoolSourceFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read file: %w", err)
	}
	return dettoolSourceBytesToString(data, "source file "+path)
}

func dettoolSourceBytesToString(data []byte, label string) (string, bool, error) {
	if len(data) == 0 {
		return "", false, fmt.Errorf("%s is empty", label)
	}
	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("%s must be valid UTF-8", label)
	}
	return string(data), true, nil
}

func runDettoolList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var tools []map[string]any
	if err := client.GetJSON(ctx, "/api/deterministic-tools", &tools); err != nil {
		return fmt.Errorf("list deterministic tools: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, tools)
	}
	printDettoolTable(tools)
	return nil
}

func runDettoolGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var tool map[string]any
	if err := client.GetJSON(ctx, "/api/deterministic-tools/"+args[0], &tool); err != nil {
		return fmt.Errorf("get deterministic tool: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, tool)
	}
	printDettoolTable([]map[string]any{tool})
	return nil
}

func runDettoolCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	body := map[string]any{"name": name}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	source, hasSource, err := resolveDettoolSourceFlag(cmd)
	if err != nil {
		return err
	}
	if hasSource {
		body["source"] = source
	}
	if cmd.Flags().Changed("enabled") {
		enabled, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = enabled
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/deterministic-tools", body, &result); err != nil {
		return fmt.Errorf("create deterministic tool: %w", err)
	}
	return printDettoolMutationResult(cmd, "Deterministic tool created", result)
}

func runDettoolUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body["name"] = strings.TrimSpace(name)
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	source, hasSource, err := resolveDettoolSourceFlag(cmd)
	if err != nil {
		return err
	}
	if hasSource {
		body["source"] = source
	}
	if cmd.Flags().Changed("enabled") {
		enabled, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = enabled
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --source, --source-file, --source-stdin, or --enabled")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/deterministic-tools/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update deterministic tool: %w", err)
	}
	return printDettoolMutationResult(cmd, "Deterministic tool updated", result)
}

func runDettoolDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Printf("Are you sure you want to delete deterministic tool %s? This cannot be undone. [y/N] ", args[0])
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/deterministic-tools/"+args[0]); err != nil {
		return fmt.Errorf("delete deterministic tool: %w", err)
	}
	fmt.Printf("Deterministic tool deleted: %s\n", args[0])
	return nil
}

func runDettoolTest(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	source, hasSource, err := resolveDettoolSourceFlag(cmd)
	if err != nil {
		return err
	}
	if len(args) == 1 {
		if hasSource {
			return fmt.Errorf("pass either an id or source flags, not both")
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		var tool map[string]any
		if err := client.GetJSON(ctx, "/api/deterministic-tools/"+args[0], &tool); err != nil {
			return fmt.Errorf("get deterministic tool: %w", err)
		}
		source = strVal(tool, "source")
		hasSource = true
	}
	if !hasSource || strings.TrimSpace(source) == "" {
		return fmt.Errorf("tool source is required; pass an id, --source, --source-file, or --source-stdin")
	}
	input, err := resolveDettoolInput(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{"source": source, "input": input}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/deterministic-tools/test", body, &result); err != nil {
		return fmt.Errorf("test deterministic tool: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	headers := []string{"STATUS", "SUMMARY", "ERROR_CODE", "RETRYABLE"}
	rows := [][]string{{
		strVal(result, "status"),
		strVal(result, "summary"),
		strVal(result, "error_code"),
		strVal(result, "retryable"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runDettoolImportFile(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	source, _, err := readDettoolSourceFile(args[0])
	if err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	name = strings.TrimSpace(name)
	if name == "" {
		name = dettoolNameFromPath(args[0])
	}
	if name == "" {
		return fmt.Errorf("could not infer tool name from %q; pass --name", args[0])
	}
	updateExisting, _ := cmd.Flags().GetBool("update-existing")

	body := map[string]any{"name": name, "source": source}
	if cmd.Flags().Changed("description") {
		desc, _ := cmd.Flags().GetString("description")
		body["description"] = desc
	}
	if cmd.Flags().Changed("enabled") {
		enabled, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = enabled
	} else {
		body["enabled"] = true
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	err = client.PostJSON(ctx, "/api/deterministic-tools", body, &result)
	if err != nil && updateExisting && isDettoolHTTPStatus(err, http.StatusConflict) {
		if existing, ok, findErr := findDettoolByName(ctx, client, name); findErr != nil {
			return fmt.Errorf("find existing deterministic tool by name: %w", findErr)
		} else if ok {
			updateBody := map[string]any{"source": source}
			if cmd.Flags().Changed("description") {
				updateBody["description"] = body["description"]
			}
			if cmd.Flags().Changed("enabled") {
				updateBody["enabled"] = body["enabled"]
			}
			if putErr := client.PutJSON(ctx, "/api/deterministic-tools/"+strVal(existing, "id"), updateBody, &result); putErr != nil {
				return fmt.Errorf("update deterministic tool from file: %w", putErr)
			}
			return printDettoolMutationResult(cmd, "Deterministic tool updated", result)
		}
	}
	if err != nil {
		if handleDettoolCreateConflict(cmd, err) {
			return nil
		}
		return fmt.Errorf("import deterministic tool file: %w", err)
	}
	return printDettoolMutationResult(cmd, "Deterministic tool created", result)
}

func resolveDettoolInput(cmd *cobra.Command) (map[string]any, error) {
	inline, _ := cmd.Flags().GetString("input")
	filePath, _ := cmd.Flags().GetString("input-file")
	if filePath != "" && cmd.Flags().Changed("input") {
		return nil, fmt.Errorf("--input and --input-file are mutually exclusive")
	}
	raw := inline
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read --input-file: %w", err)
		}
		raw = string(data)
	}
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, fmt.Errorf("input must be a JSON object: %w", err)
	}
	if input == nil {
		input = map[string]any{}
	}
	return input, nil
}

func dettoolNameFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	base = strings.ReplaceAll(base, "-", "_")
	return base
}

func findDettoolByName(ctx context.Context, client *cli.APIClient, name string) (map[string]any, bool, error) {
	var tools []map[string]any
	if err := client.GetJSON(ctx, "/api/deterministic-tools", &tools); err != nil {
		return nil, false, fmt.Errorf("list deterministic tools: %w", err)
	}
	for _, tool := range tools {
		if strVal(tool, "name") == name {
			return tool, true, nil
		}
	}
	return nil, false, nil
}

func isDettoolHTTPStatus(err error, status int) bool {
	var httpErr *cli.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == status
}

func handleDettoolCreateConflict(cmd *cobra.Command, err error) bool {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return false
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		_ = cli.PrintJSON(os.Stdout, map[string]any{"error": strings.TrimSpace(httpErr.Body)})
		return true
	}
	fmt.Println(strings.TrimSpace(httpErr.Body))
	return true
}

func printDettoolMutationResult(cmd *cobra.Command, message string, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("%s: %s (%s)\n", message, strVal(result, "name"), strVal(result, "id"))
	return nil
}

func printDettoolTable(tools []map[string]any) {
	headers := []string{"ID", "NAME", "ENABLED", "DESCRIPTION", "UPDATED_AT"}
	rows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		rows = append(rows, []string{
			strVal(tool, "id"),
			strVal(tool, "name"),
			strVal(tool, "enabled"),
			strVal(tool, "description"),
			strVal(tool, "updated_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
