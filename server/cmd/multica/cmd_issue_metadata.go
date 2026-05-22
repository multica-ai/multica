package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// buildMetadataFilterQueryParam converts repeated `--metadata key=value`
// flags into the canonical JSON object passed as the `metadata` query
// parameter to /api/issues. Values are typed by the same rules as
// `metadata set --value` without --type — JSON-parsed when possible, else
// taken as a literal string. Duplicate keys are rejected; AND semantics
// require distinct keys.
func buildMetadataFilterQueryParam(pairs []string) (string, error) {
	if len(pairs) == 0 {
		return "", nil
	}
	out := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		idx := strings.IndexByte(pair, '=')
		if idx <= 0 {
			return "", fmt.Errorf("--metadata %q must be in key=value form", pair)
		}
		key := pair[:idx]
		raw := pair[idx+1:]
		if _, dup := out[key]; dup {
			return "", fmt.Errorf("--metadata key %q given more than once; combine into a single filter", key)
		}
		encoded, err := parseMetadataValue(raw, "")
		if err != nil {
			return "", fmt.Errorf("--metadata %s: %w", key, err)
		}
		var v any
		if err := json.Unmarshal(encoded, &v); err != nil {
			return "", fmt.Errorf("--metadata %s: encode value: %w", key, err)
		}
		out[key] = v
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode metadata filter: %w", err)
	}
	return string(buf), nil
}

// multica issue metadata {list|get|set|delete} — KV map attached to each issue
// for agent pipeline state. See server/internal/handler/issue_metadata.go for
// the constraints (key regex, 50-key cap, primitive-only values, 8KB blob).

var issueMetadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Manage per-issue metadata (KV)",
}

var issueMetadataListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List all metadata keys on an issue",
	Args:  exactArgs(1),
	RunE:  runIssueMetadataList,
}

var issueMetadataGetCmd = &cobra.Command{
	Use:   "get <issue-id>",
	Short: "Get a single metadata key value",
	Args:  exactArgs(1),
	RunE:  runIssueMetadataGet,
}

var issueMetadataSetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Set a single metadata key value",
	Long: `Set a single metadata key value. The value is JSON-parsed by default:
  --value true / --value false  → bool
  --value 3 / --value 3.14      → number
  --value waiting               → string
Use --type to force a specific type. Quote like '"42"' (JSON-escaped) to force
a string when the bare value would otherwise sniff as a number or bool.`,
	Args: exactArgs(1),
	RunE: runIssueMetadataSet,
}

var issueMetadataDeleteCmd = &cobra.Command{
	Use:   "delete <issue-id>",
	Short: "Delete a single metadata key",
	Args:  exactArgs(1),
	RunE:  runIssueMetadataDelete,
}

func init() {
	issueMetadataCmd.AddCommand(issueMetadataListCmd)
	issueMetadataCmd.AddCommand(issueMetadataGetCmd)
	issueMetadataCmd.AddCommand(issueMetadataSetCmd)
	issueMetadataCmd.AddCommand(issueMetadataDeleteCmd)

	issueMetadataListCmd.Flags().String("output", "table", "Output format: table or json")
	issueMetadataGetCmd.Flags().String("output", "json", "Output format: table or json")
	issueMetadataGetCmd.Flags().String("key", "", "Metadata key (required)")
	issueMetadataSetCmd.Flags().String("output", "table", "Output format: table or json")
	issueMetadataSetCmd.Flags().String("key", "", "Metadata key (required)")
	issueMetadataSetCmd.Flags().String("value", "", "Metadata value (required)")
	issueMetadataSetCmd.Flags().String("type", "", "Force value type: string, number, or bool (default: auto-infer via JSON parsing)")
	issueMetadataDeleteCmd.Flags().String("output", "table", "Output format: table or json")
	issueMetadataDeleteCmd.Flags().String("key", "", "Metadata key (required)")

	issueCmd.AddCommand(issueMetadataCmd)
}

// parseMetadataValue converts a CLI --value flag (and optional --type) into
// the JSON-encoded payload sent to the server. Default sniffing: if the
// raw text parses as a JSON bool / number / string, that type is used; any
// other JSON shape (null, array, object) is rejected even under default
// because the server would reject it too.
//
// forcedType non-empty overrides sniffing: "string" wraps verbatim as a
// JSON string; "number" parses strictly as a number; "bool" requires the
// literal text "true" or "false".
func parseMetadataValue(raw, forcedType string) (json.RawMessage, error) {
	switch forcedType {
	case "string":
		buf, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("encode string value: %w", err)
		}
		return buf, nil
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("value %q is not a valid number", raw)
		}
		return json.RawMessage(raw), nil
	case "bool":
		if raw != "true" && raw != "false" {
			return nil, fmt.Errorf("value %q is not a valid bool (expected true or false)", raw)
		}
		return json.RawMessage(raw), nil
	case "":
		// auto-infer below
	default:
		return nil, fmt.Errorf("unknown --type %q (expected string, number, or bool)", forcedType)
	}

	// Auto-infer: try JSON parse, fall back to string.
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		switch v.(type) {
		case string, bool, float64:
			return json.RawMessage(raw), nil
		}
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode string value: %w", err)
	}
	return buf, nil
}

func runIssueMetadataList(cmd *cobra.Command, args []string) error {
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

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/metadata", &result); err != nil {
		return fmt.Errorf("list metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func runIssueMetadataGet(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
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

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/metadata", &result); err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	value, present := metadata[key]
	if !present {
		return fmt.Errorf("key %q not found on issue", key)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, value)
	}
	headers := []string{"KEY", "VALUE", "TYPE"}
	rows := [][]string{{key, formatMetadataValue(value), metadataValueType(value)}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueMetadataSet(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}
	if !cmd.Flags().Changed("value") {
		return fmt.Errorf("--value is required")
	}
	rawValue, _ := cmd.Flags().GetString("value")
	forcedType, _ := cmd.Flags().GetString("type")
	value, err := parseMetadataValue(rawValue, forcedType)
	if err != nil {
		return err
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

	body := map[string]any{"value": value}
	var result map[string]any
	path := "/api/issues/" + issueRef.ID + "/metadata/" + key
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("set metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func runIssueMetadataDelete(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
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

	path := "/api/issues/" + issueRef.ID + "/metadata/" + key
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	// Refresh the metadata so the user sees the result.
	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/metadata", &result); err != nil {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"deleted": true})
		}
		fmt.Fprintln(os.Stdout, "Key deleted.")
		return nil
	}
	metadata, _ := result["metadata"].(map[string]any)
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func printMetadataTable(metadata map[string]any) {
	headers := []string{"KEY", "VALUE", "TYPE"}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		v := metadata[k]
		rows = append(rows, []string{k, formatMetadataValue(v), metadataValueType(v)})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func formatMetadataValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers come back as float64; render integers without a
		// trailing ".0" for ergonomics.
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		buf, _ := json.Marshal(v)
		return string(buf)
	}
}

func metadataValueType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	default:
		return "unknown"
	}
}
