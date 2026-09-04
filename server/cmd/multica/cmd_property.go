package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica property {list|get|create|update|archive|unarchive} — workspace
// custom property definitions, and multica issue property {list|set|unset} —
// typed values on a single issue. See server/internal/handler/property.go
// for the validation contract (9 types, 20 active definitions/workspace,
// owner/admin-only definition management, agents rejected on definition
// writes).
//
// CLI ergonomics: properties, select options and actor values are addressed BY
// NAME (case-insensitive); the CLI translates names to the UUIDs the API
// expects, so agents never have to juggle option ids or actor references.

type propertyOptionDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type propertyDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Config      struct {
		Options []propertyOptionDTO `json:"options"`
	} `json:"config"`
	Position   float64 `json:"position"`
	Archived   bool    `json:"archived"`
	UsageCount int64   `json:"usage_count"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

var propertyCmd = &cobra.Command{
	Use:   "property",
	Short: "Manage workspace custom issue properties",
}

var propertyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List property definitions",
	Args:  exactArgs(0),
	RunE:  runPropertyList,
}

var propertyGetCmd = &cobra.Command{
	Use:   "get <id-or-name>",
	Short: "Show one property definition",
	Args:  exactArgs(1),
	RunE:  runPropertyGet,
}

var propertyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a property definition (workspace owner/admin only)",
	Long: `Create a property definition. Types: text, number, select, multi_select,
date, checkbox, url, actor, multi_actor. Select types take repeatable --option
flags:
  multica property create --name Severity --type select \
      --option "Critical:#ef4444" --option "Major:#f59e0b" --option "Minor:#6b7280"
The ":#rrggbb" color suffix is optional.

The actor types hold workspace members; they take no options:
  multica property create --name Reviewer --type actor`,
	Args: exactArgs(0),
	RunE: runPropertyCreate,
}

var propertyUpdateCmd = &cobra.Command{
	Use:   "update <id-or-name>",
	Short: "Update a property definition (owner/admin only; type is immutable)",
	Long: `Update a property definition. --option flags REPLACE the full option list;
existing options are matched by name so their ids (and issue values) survive.`,
	Args: exactArgs(1),
	RunE: runPropertyUpdate,
}

var propertyArchiveCmd = &cobra.Command{
	Use:   "archive <id-or-name>",
	Short: "Archive a property definition (hidden from pickers; values preserved)",
	Args:  exactArgs(1),
	RunE:  makePropertyArchiveRun(true),
}

var propertyUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id-or-name>",
	Short: "Restore an archived property definition",
	Args:  exactArgs(1),
	RunE:  makePropertyArchiveRun(false),
}

var issuePropertyCmd = &cobra.Command{
	Use:   "property",
	Short: "Manage custom property values on an issue",
}

var issuePropertyListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List custom property values set on an issue",
	Args:  exactArgs(1),
	RunE:  runIssuePropertyList,
}

var issuePropertySetCmd = &cobra.Command{
	Use:   "set <issue-id>",
	Short: "Set a custom property value on an issue",
	Long: `Set a custom property value. The property is addressed by --name
(case-insensitive) or UUID. Value forms by type:
  select        --value Staging            (option name or id)
  multi_select  --value "iOS,Android"      (comma-separated option names or ids)
  actor         --value Bohan             (member name, email, or id)
  multi_actor   --value "Bohan,Jiayuan"   (comma-separated members)
  checkbox      --value true|false
  number        --value 3.5
  date          --value 2026-07-13
  text / url    --value "any string"`,
	Args: exactArgs(1),
	RunE: runIssuePropertySet,
}

var issuePropertyUnsetCmd = &cobra.Command{
	Use:   "unset <issue-id>",
	Short: "Remove a custom property value from an issue",
	Args:  exactArgs(1),
	RunE:  runIssuePropertyUnset,
}

func init() {
	propertyCmd.AddCommand(propertyListCmd)
	propertyCmd.AddCommand(propertyGetCmd)
	propertyCmd.AddCommand(propertyCreateCmd)
	propertyCmd.AddCommand(propertyUpdateCmd)
	propertyCmd.AddCommand(propertyArchiveCmd)
	propertyCmd.AddCommand(propertyUnarchiveCmd)

	propertyListCmd.Flags().String("output", "table", "Output format: table or json")
	propertyListCmd.Flags().Bool("include-archived", false, "Include archived properties")
	propertyGetCmd.Flags().String("output", "json", "Output format: table or json")
	propertyCreateCmd.Flags().String("output", "table", "Output format: table or json")
	propertyCreateCmd.Flags().String("name", "", "Property name (required)")
	propertyCreateCmd.Flags().String("type", "", "Property type: text, number, select, multi_select, date, checkbox, url, actor, multi_actor (required)")
	propertyCreateCmd.Flags().String("description", "", "Property description")
	propertyCreateCmd.Flags().String("icon", "", "Property icon key from the Web picker (for example, flag, tag, or shield)")
	propertyCreateCmd.Flags().StringArray("option", nil, `Select option as "Name" or "Name:#rrggbb" (repeatable; select types only)`)
	propertyUpdateCmd.Flags().String("output", "table", "Output format: table or json")
	propertyUpdateCmd.Flags().String("name", "", "New property name")
	propertyUpdateCmd.Flags().String("description", "", "New property description")
	propertyUpdateCmd.Flags().String("icon", "", "New property icon key from the Web picker; pass an empty value to clear")
	propertyUpdateCmd.Flags().StringArray("option", nil, `Replacement option list as "Name" or "Name:#rrggbb" (repeatable)`)
	propertyArchiveCmd.Flags().String("output", "table", "Output format: table or json")
	propertyUnarchiveCmd.Flags().String("output", "table", "Output format: table or json")

	issuePropertyCmd.AddCommand(issuePropertyListCmd)
	issuePropertyCmd.AddCommand(issuePropertySetCmd)
	issuePropertyCmd.AddCommand(issuePropertyUnsetCmd)

	issuePropertyListCmd.Flags().String("output", "table", "Output format: table or json")
	issuePropertySetCmd.Flags().String("output", "table", "Output format: table or json")
	issuePropertySetCmd.Flags().String("name", "", "Property name or UUID (required)")
	issuePropertySetCmd.Flags().String("value", "", "Property value (required; see --help for per-type forms)")
	issuePropertyUnsetCmd.Flags().String("output", "table", "Output format: table or json")
	issuePropertyUnsetCmd.Flags().String("name", "", "Property name or UUID (required)")

	issueCmd.AddCommand(issuePropertyCmd)
}

// fetchProperties loads the full definition catalog (including archived — the
// callers that must exclude archived filter locally, and value resolution for
// display needs archived definitions too).
func fetchProperties(ctx context.Context, client *cli.APIClient) ([]propertyDTO, error) {
	var result struct {
		Properties []propertyDTO `json:"properties"`
	}
	if err := client.GetJSON(ctx, "/api/properties?include_archived=true", &result); err != nil {
		return nil, fmt.Errorf("list properties: %w", err)
	}
	return result.Properties, nil
}

// resolvePropertyRef matches a CLI ref against the catalog by UUID first,
// then case-insensitive name.
func resolvePropertyRef(properties []propertyDTO, ref string) (propertyDTO, error) {
	for _, p := range properties {
		if p.ID == ref {
			return p, nil
		}
	}
	lower := strings.ToLower(strings.TrimSpace(ref))
	for _, p := range properties {
		if strings.ToLower(p.Name) == lower {
			return p, nil
		}
	}
	names := make([]string, len(properties))
	for i, p := range properties {
		names[i] = p.Name
	}
	return propertyDTO{}, fmt.Errorf("property %q not found; available: %s", ref, strings.Join(names, ", "))
}

// parseOptionFlags converts repeatable --option flags ("Name" or
// "Name:#rrggbb") into config options. When updating, pass the existing
// options so same-named options keep their ids (issue values reference ids).
const defaultOptionColor = "#6b7280"

func parseOptionFlags(flags []string, existing []propertyOptionDTO) []map[string]string {
	byName := make(map[string]string, len(existing))
	for _, opt := range existing {
		byName[strings.ToLower(opt.Name)] = opt.ID
	}
	out := make([]map[string]string, 0, len(flags))
	for _, raw := range flags {
		name := raw
		color := defaultOptionColor
		if idx := strings.LastIndex(raw, ":#"); idx > 0 {
			name = raw[:idx]
			color = raw[idx+1:]
		}
		name = strings.TrimSpace(name)
		opt := map[string]string{"name": name, "color": color}
		if id, ok := byName[strings.ToLower(name)]; ok {
			opt["id"] = id
		}
		out = append(out, opt)
	}
	return out
}

func printPropertyTable(properties []propertyDTO) {
	headers := []string{"ID", "ICON", "NAME", "TYPE", "OPTIONS", "USED", "ARCHIVED"}
	rows := make([][]string, 0, len(properties))
	for _, p := range properties {
		names := make([]string, len(p.Config.Options))
		for i, opt := range p.Config.Options {
			names[i] = opt.Name
		}
		archived := ""
		if p.Archived {
			archived = "yes"
		}
		rows = append(rows, []string{p.ID, p.Icon, p.Name, p.Type, strings.Join(names, ", "), strconv.FormatInt(p.UsageCount, 10), archived})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func runPropertyList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	path := "/api/properties"
	if includeArchived {
		path += "?include_archived=true"
	}
	var result struct {
		Properties []propertyDTO `json:"properties"`
	}
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list properties: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Properties)
	}
	printPropertyTable(result.Properties)
	return nil
}

func runPropertyGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, args[0])
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, property)
	}
	printPropertyTable([]propertyDTO{property})
	return nil
}

func runPropertyCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	propType, _ := cmd.Flags().GetString("type")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if propType == "" {
		return fmt.Errorf("--type is required")
	}
	description, _ := cmd.Flags().GetString("description")
	icon, _ := cmd.Flags().GetString("icon")
	optionFlags, _ := cmd.Flags().GetStringArray("option")

	body := map[string]any{"name": name, "type": propType, "description": description, "icon": icon}
	if len(optionFlags) > 0 {
		body["config"] = map[string]any{"options": parseOptionFlags(optionFlags, nil)}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var created propertyDTO
	if err := client.PostJSON(ctx, "/api/properties", body, &created); err != nil {
		return fmt.Errorf("create property: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, created)
	}
	fmt.Fprintf(os.Stdout, "Property %q created.\n", created.Name)
	printPropertyTable([]propertyDTO{created})
	return nil
}

func runPropertyUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, args[0])
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body["name"] = name
	}
	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		body["description"] = description
	}
	if cmd.Flags().Changed("icon") {
		icon, _ := cmd.Flags().GetString("icon")
		body["icon"] = icon
	}
	if cmd.Flags().Changed("option") {
		optionFlags, _ := cmd.Flags().GetStringArray("option")
		body["config"] = map[string]any{"options": parseOptionFlags(optionFlags, property.Config.Options)}
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update; pass --name, --description, --icon, or --option")
	}

	var updated propertyDTO
	if err := client.PatchJSON(ctx, "/api/properties/"+property.ID, body, &updated); err != nil {
		return fmt.Errorf("update property: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, updated)
	}
	fmt.Fprintf(os.Stdout, "Property %q updated.\n", updated.Name)
	printPropertyTable([]propertyDTO{updated})
	return nil
}

func makePropertyArchiveRun(archive bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()

		properties, err := fetchProperties(ctx, client)
		if err != nil {
			return err
		}
		property, err := resolvePropertyRef(properties, args[0])
		if err != nil {
			return err
		}
		var updated propertyDTO
		if err := client.PatchJSON(ctx, "/api/properties/"+property.ID, map[string]any{"archived": archive}, &updated); err != nil {
			if archive {
				return fmt.Errorf("archive property: %w", err)
			}
			return fmt.Errorf("unarchive property: %w", err)
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			return cli.PrintJSON(os.Stdout, updated)
		}
		if archive {
			fmt.Fprintf(os.Stdout, "Property %q archived.\n", updated.Name)
		} else {
			fmt.Fprintf(os.Stdout, "Property %q restored.\n", updated.Name)
		}
		return nil
	}
}

// ---------------------------------------------------------------------------
// issue property {list|set|unset}
// ---------------------------------------------------------------------------

// resolveActorPropertyRef turns one --value token into a "member:<uuid>"
// actor reference. An already-prefixed token is taken as-is (after checking
// the id parses); anything else goes through the same member lookup
// `--assignee` uses, so names, emails, UUIDs and short ids all work.
func resolveActorPropertyRef(ctx context.Context, client *cli.APIClient, raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", fmt.Errorf("actor value cannot be empty")
	}
	if kind, id, found := strings.Cut(token, ":"); found && kind == "member" {
		parsed, err := uuid.Parse(strings.TrimSpace(id))
		if err != nil {
			return "", fmt.Errorf("actor id in %q must be a UUID", token)
		}
		// Return the canonical lowercase-hyphenated spelling. Stored actor
		// values are normalized on write, and the issue list filter matches
		// the stored string exactly — an uppercase or braced input would
		// store fine via `property set` but silently miss as a filter.
		return kind + ":" + parsed.String(), nil
	}
	actorType, actorID, err := resolveAssignee(ctx, client, token, memberOnlyKinds)
	if err != nil {
		return "", err
	}
	return actorType + ":" + actorID, nil
}

// resolveSelectOptionRef matches a select/multi_select value reference
// against a definition's options by id first, then case-insensitive name —
// the same addressing contract resolvePropertyRef gives definitions.
func resolveSelectOptionRef(property propertyDTO, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	for _, opt := range property.Config.Options {
		if opt.ID == ref || strings.EqualFold(opt.Name, ref) {
			return opt.ID, nil
		}
	}
	optionNames := make([]string, len(property.Config.Options))
	for i, opt := range property.Config.Options {
		optionNames[i] = opt.Name
	}
	return "", fmt.Errorf("option %q not found on property %q; valid options: %s", ref, property.Name, strings.Join(optionNames, ", "))
}

// encodeIssuePropertyValue converts the CLI --value string into the typed
// JSON the API expects, translating option names to ids for select types and
// member names to actor references for actor types.
func encodeIssuePropertyValue(ctx context.Context, client *cli.APIClient, property propertyDTO, raw string) (json.RawMessage, error) {
	optionNames := make([]string, len(property.Config.Options))
	for i, opt := range property.Config.Options {
		optionNames[i] = opt.Name
	}

	switch property.Type {
	case "select":
		id, err := resolveSelectOptionRef(property, raw)
		if err != nil {
			return nil, err
		}
		return json.Marshal(id)
	case "multi_select":
		parts := strings.Split(raw, ",")
		ids := make([]string, 0, len(parts))
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			id, err := resolveSelectOptionRef(property, part)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("--value must list at least one option; valid options: %s", strings.Join(optionNames, ", "))
		}
		return json.Marshal(ids)
	case "actor":
		ref, err := resolveActorPropertyRef(ctx, client, raw)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ref)
	case "multi_actor":
		parts := strings.Split(raw, ",")
		refs := make([]string, 0, len(parts))
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			ref, err := resolveActorPropertyRef(ctx, client, part)
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
		if len(refs) == 0 {
			return nil, fmt.Errorf("--value must list at least one member")
		}
		return json.Marshal(refs)
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("value %q is not a valid number", raw)
		}
		return json.RawMessage(raw), nil
	case "checkbox":
		if raw != "true" && raw != "false" {
			return nil, fmt.Errorf("value %q is not a valid bool (expected true or false)", raw)
		}
		return json.RawMessage(raw), nil
	default: // text, date, url — validated server-side
		return json.Marshal(raw)
	}
}

// formatIssuePropertyValue renders a stored value for humans: option ids
// become option names, actor references become member names, everything
// else prints via formatMetadataValue. actorNames may be nil — references then
// print in their raw "<kind>:<uuid>" form rather than failing.
func formatIssuePropertyValue(property propertyDTO, value any, actorNames map[string]string) string {
	optionName := func(id string) string {
		for _, opt := range property.Config.Options {
			if opt.ID == id {
				return opt.Name
			}
		}
		return id
	}
	actorName := func(ref string) string {
		if name, ok := actorNames[ref]; ok {
			return name
		}
		return ref
	}
	switch property.Type {
	case "select":
		if s, ok := value.(string); ok {
			return optionName(s)
		}
	case "multi_select":
		if items, ok := value.([]any); ok {
			names := make([]string, 0, len(items))
			for _, item := range items {
				if s, ok := item.(string); ok {
					names = append(names, optionName(s))
				}
			}
			return strings.Join(names, ", ")
		}
	case "actor":
		if s, ok := value.(string); ok {
			return actorName(s)
		}
	case "multi_actor":
		if items, ok := value.([]any); ok {
			names := make([]string, 0, len(items))
			for _, item := range items {
				if s, ok := item.(string); ok {
					names = append(names, actorName(s))
				}
			}
			return strings.Join(names, ", ")
		}
	case "checkbox":
		if b, ok := value.(bool); ok {
			if b {
				return "✓"
			}
			return "✗"
		}
	}
	return formatMetadataValue(value)
}

type issuePropertyValueRow struct {
	PropertyID string `json:"property_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Value      any    `json:"value"`
	Display    string `json:"display"`
	Archived   bool   `json:"archived,omitempty"`
}

func buildIssuePropertyRows(properties []propertyDTO, bag map[string]any, actorNames map[string]string) []issuePropertyValueRow {
	rows := make([]issuePropertyValueRow, 0, len(bag))
	for _, p := range properties {
		value, present := bag[p.ID]
		if !present {
			continue
		}
		rows = append(rows, issuePropertyValueRow{
			PropertyID: p.ID,
			Name:       p.Name,
			Type:       p.Type,
			Value:      value,
			Display:    formatIssuePropertyValue(p, value, actorNames),
			Archived:   p.Archived,
		})
	}
	return rows
}

// fetchActorPropertyNames builds a "member:<uuid>" → display name map, but
// only when the bag actually holds an actor value: every other property type
// renders without a second round trip, and `issue property list` shouldn't pay
// for a request it doesn't need.
func fetchActorPropertyNames(ctx context.Context, client *cli.APIClient, properties []propertyDTO, bag map[string]any) map[string]string {
	needed := false
	for _, p := range properties {
		if _, present := bag[p.ID]; present && (p.Type == "actor" || p.Type == "multi_actor") {
			needed = true
			break
		}
	}
	if !needed || client.WorkspaceID == "" {
		return nil
	}
	names := make(map[string]string)
	var members []map[string]any
	if err := getAssigneeJSON(ctx, client, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err == nil {
		for _, m := range members {
			if id := strVal(m, "user_id"); id != "" {
				names["member:"+id] = strVal(m, "name")
			}
		}
	}
	return names
}

func fetchIssuePropertyBag(ctx context.Context, client *cli.APIClient, issueID string) (map[string]any, error) {
	var issue struct {
		Properties map[string]any `json:"properties"`
	}
	if err := client.GetJSON(ctx, "/api/issues/"+issueID, &issue); err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	if issue.Properties == nil {
		return map[string]any{}, nil
	}
	return issue.Properties, nil
}

func printIssuePropertyRows(rows []issuePropertyValueRow) {
	headers := []string{"NAME", "VALUE", "TYPE"}
	tableRows := make([][]string, len(rows))
	for i, row := range rows {
		tableRows[i] = []string{row.Name, row.Display, row.Type}
	}
	cli.PrintTable(os.Stdout, headers, tableRows)
}

func runIssuePropertyList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	bag, err := fetchIssuePropertyBag(ctx, client, issueRef.ID)
	if err != nil {
		return err
	}
	rows := buildIssuePropertyRows(properties, bag, fetchActorPropertyNames(ctx, client, properties, bag))
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	printIssuePropertyRows(rows)
	return nil
}

func runIssuePropertySet(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if !cmd.Flags().Changed("value") {
		return fmt.Errorf("--value is required")
	}
	rawValue, _ := cmd.Flags().GetString("value")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, name)
	if err != nil {
		return err
	}
	value, err := encodeIssuePropertyValue(ctx, client, property, rawValue)
	if err != nil {
		return err
	}

	var result struct {
		Properties map[string]any `json:"properties"`
	}
	path := "/api/issues/" + issueRef.ID + "/properties/" + property.ID
	if err := client.PutJSON(ctx, path, map[string]any{"value": value}, &result); err != nil {
		return fmt.Errorf("set property: %w", err)
	}
	rows := buildIssuePropertyRows(properties, result.Properties, fetchActorPropertyNames(ctx, client, properties, result.Properties))
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	printIssuePropertyRows(rows)
	return nil
}

func runIssuePropertyUnset(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	properties, err := fetchProperties(ctx, client)
	if err != nil {
		return err
	}
	property, err := resolvePropertyRef(properties, name)
	if err != nil {
		return err
	}

	path := "/api/issues/" + issueRef.ID + "/properties/" + property.ID
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("unset property: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Property %q unset.\n", property.Name)
	return nil
}

// ---------------------------------------------------------------------------
// issue list --property / --sort property:<ref>
// ---------------------------------------------------------------------------

// propertyNoValueSentinel is the server's reserved filter value meaning "the
// property is not set" (see parsePropertiesFilterParam in
// internal/handler/property.go). It short-circuits value resolution for every
// property type, so an option or member literally named "__none__" can only be
// filtered by its UUID.
const propertyNoValueSentinel = "__none__"

// Store-side caps from validatePropertyValue in internal/handler/property.go.
// A filter value past them could never match anything.
const (
	maxPropertyTextValueLen = 2000 // runes
	maxPropertyURLValueLen  = 2048 // bytes
)

// issueSortablePropertyTypes are the property types the server gives a
// meaningful ORDER BY (see propertySortExpr). This is an allowlist on purpose:
// a type the CLI does not know — multi_select, checkbox, actor kinds, or a
// future type from a newer backend — would be silently degraded to position
// order by the server, and a passed-but-ignored flag is a footgun in scripts.
var issueSortablePropertyTypes = []string{"select", "number", "date", "text", "url"}

// buildPropertiesFilterQueryParam converts repeated `--property Name=Value`
// flags into the JSON object passed as the `properties` query parameter to
// /api/issues. Each flag carries exactly one value; repeating the same
// property ORs its values (server semantics: OR within a definition, AND
// across definitions), keyed by the RESOLVED definition id so name and UUID
// addressing aggregate into one entry.
func buildPropertiesFilterQueryParam(ctx context.Context, client *cli.APIClient, properties []propertyDTO, pairs []string) (string, error) {
	filter := make(map[string][]string, len(pairs))
	for _, pair := range pairs {
		name, rawValue, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(name) == "" {
			return "", fmt.Errorf(`--property %q must be in "Name=Value" form`, pair)
		}
		// Reserved so scripts never come to depend on "Impact>" resolving as a
		// property name once >=, <=, != mean comparison filters.
		if n := strings.TrimSpace(name); strings.HasSuffix(n, "<") || strings.HasSuffix(n, ">") || strings.HasSuffix(n, "!") {
			return "", fmt.Errorf(`--property %q: comparison operators are not supported yet; only "Name=Value" is accepted`, pair)
		}
		if strings.TrimSpace(rawValue) == "" {
			return "", fmt.Errorf("--property %s: value cannot be empty (use %s to match issues where the property is unset)", name, propertyNoValueSentinel)
		}
		property, err := resolvePropertyRef(properties, name)
		if err != nil {
			return "", err
		}
		if property.Archived {
			return "", fmt.Errorf("property %q is archived; archived properties are hidden from filtering (matching the web UI) — restore it with `multica property unarchive` if you need it", property.Name)
		}
		value, err := resolvePropertyFilterValue(ctx, client, property, rawValue)
		if err != nil {
			return "", err
		}
		duplicate := false
		for _, existing := range filter[property.ID] {
			if existing == value {
				duplicate = true
				break
			}
		}
		if !duplicate {
			filter[property.ID] = append(filter[property.ID], value)
		}
	}
	buf, err := json.Marshal(filter)
	if err != nil {
		return "", fmt.Errorf("encode properties filter: %w", err)
	}
	return string(buf), nil
}

// resolvePropertyFilterValue turns one human-facing filter value into the
// string the server matches stored values against. The CLI filters the same
// property types the web UI offers — see isFilterablePropertyType in
// packages/core/types/property.ts.
//
// Scalars match by exact containment, so whatever we send has to be spelled
// the way the value was stored. validatePropertyValue keeps text as written,
// trims url and only stores http(s), holds date to YYYY-MM-DD, and caps text
// and url length; the branches below follow it. A value that could never
// match is rejected here rather than sent, because an empty result reads
// like a real answer.
func resolvePropertyFilterValue(ctx context.Context, client *cli.APIClient, property propertyDTO, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == propertyNoValueSentinel {
		return propertyNoValueSentinel, nil
	}
	switch property.Type {
	case "select", "multi_select":
		return resolveSelectOptionRef(property, trimmed)
	case "checkbox":
		if trimmed != "true" && trimmed != "false" {
			return "", fmt.Errorf("--property %s: value %q is not a valid bool (expected true or false)", property.Name, trimmed)
		}
		return trimmed, nil
	case "actor", "multi_actor":
		return resolveActorPropertyRef(ctx, client, trimmed)
	case "number":
		num, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return "", fmt.Errorf("--property %s: value %q is not a valid number", property.Name, trimmed)
		}
		if math.IsNaN(num) || math.IsInf(num, 0) {
			// NaN and infinity have no JSON spelling, so the server never
			// builds a numeric match for them and the filter would come
			// back empty for the wrong reason.
			return "", fmt.Errorf("--property %s: value %q is not a finite number", property.Name, trimmed)
		}
		return trimmed, nil
	case "date":
		if _, err := time.Parse("2006-01-02", trimmed); err != nil {
			return "", fmt.Errorf("--property %s: value %q is not a date in YYYY-MM-DD form", property.Name, trimmed)
		}
		return trimmed, nil
	case "url":
		if len(trimmed) > maxPropertyURLValueLen {
			return "", fmt.Errorf("--property %s: value must be %d characters or fewer", property.Name, maxPropertyURLValueLen)
		}
		if u, err := url.Parse(trimmed); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", fmt.Errorf("--property %s: value %q is not an http(s) URL", property.Name, trimmed)
		}
		return trimmed, nil
	case "text":
		if utf8.RuneCountInString(raw) > maxPropertyTextValueLen {
			return "", fmt.Errorf("--property %s: value must be %d characters or fewer", property.Name, maxPropertyTextValueLen)
		}
		// Text is stored exactly as written, so trimming here would miss a
		// value that genuinely has spaces around it.
		return raw, nil
	default: // a property type only a newer backend knows about
		return "", fmt.Errorf("--property %s: this CLI does not know how to filter %s properties; update with `multica update`, or use the %s unset filter", property.Name, property.Type, propertyNoValueSentinel)
	}
}

// resolveSortableProperty resolves a `--sort property:<ref>` target and
// applies the loud-failure guards for cases the server would silently degrade
// to position order: archived definitions and types with no sort order.
func resolveSortableProperty(properties []propertyDTO, ref string) (propertyDTO, error) {
	property, err := resolvePropertyRef(properties, ref)
	if err != nil {
		return propertyDTO{}, err
	}
	if property.Archived {
		return propertyDTO{}, fmt.Errorf("property %q is archived and the server would fall back to position order; restore it first with `multica property unarchive`", property.Name)
	}
	for _, sortable := range issueSortablePropertyTypes {
		if property.Type == sortable {
			return property, nil
		}
	}
	return propertyDTO{}, fmt.Errorf("%s property %q has no server-side sort order; sortable property types: %s", property.Type, property.Name, strings.Join(issueSortablePropertyTypes, ", "))
}
