package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// View write commands — create, update, duplicate and delete saved views.
//
// A view's query and display are the web client's IssueViewDefinition
// (packages/core/issue-views/definition.ts). The client merges a partial
// definition over its defaults when it opens a view, so these commands write
// only the keys a flag sets and keep every other key as stored.

// issueViewDefinitionVersion is the definition format this CLI writes. A view
// stored in any other format is refused instead of rewritten in this one.
const issueViewDefinitionVersion = 1

const issueViewNameMaxLen = 80

var (
	viewScopeVariants = map[string][]string{
		"workspace": {"all", "members", "agents"},
		"project":   {"all", "members", "agents"},
		"my":        {"any", "assigned", "created", "involved"},
	}
	viewVisibilities      = []string{"private", "workspace"}
	viewModes             = []string{"board", "list", "table", "gantt", "swimlane"}
	viewGroupings         = []string{"status", "assignee", "project"}
	viewTableGroupings    = []string{"none", "status", "assignee", "project"}
	viewSwimlaneGroupings = []string{"parent", "project", "assignee"}
	viewGanttZooms        = []string{"day", "week", "month"}
	viewSortFields        = []string{"position", "status", "priority", "start_date", "due_date", "created_at", "updated_at", "title"}
	viewRelations         = []string{"assigned", "created", "my_agents", "subscribed"}
	viewDueDates          = []string{"overdue", "today", "this_week", "next_7_days", "none"}
	// viewNegatableDimensions maps each --negate value to its query key, in
	// the client's canonical order.
	viewNegatableDimensions = []struct{ flag, dimension string }{
		{"status", "status"},
		{"priority", "priority"},
		{"assignee", "assignee"},
		{"creator", "creator"},
		{"project", "project"},
		{"project-status", "projectStatus"},
		{"label", "label"},
		{"relation", "relation"},
		{"due", "dueDate"},
	}
)

// viewFlatFilterFlags write the simple-mode filter fields. While a view
// filters with an advanced condition tree the client keeps these fields
// empty, so they cannot be combined with one.
var viewFlatFilterFlags = []string{
	"status", "priority", "assignee", "include-no-assignee", "creator",
	"project-filter", "include-no-project", "project-status", "label",
	"relation", "due", "property", "negate",
}

var viewFilterFlags = append(slices.Clone(viewFlatFilterFlags), "agent-running", "agent-idle", "unread")

var viewDisplayFlags = []string{
	"view-mode", "grouping", "table-grouping", "swimlane-grouping", "gantt-zoom",
	"sort", "direction", "show-sub-issues",
}

var viewCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a saved view",
	Long: `Create a saved view. It is saved exactly as the web app saves one, so it shows
in the sidebar and can be opened and edited there.

Filter flags take names the way the UI shows them; the CLI resolves them to
IDs before writing. List flags are repeatable and also accept comma-separated
values. Unset flags keep the app's defaults.

--definition-file takes a JSON file with "query" and/or "display" objects,
for example the output of ` + "`multica view get`" + ` edited by hand; flags apply on
top of it. --dry-run prints the request without saving. After saving, preview
what the view lists with ` + "`multica view issues <view>`" + `.

Inside an agent run the view is created for, and owned by, the runtime owner.`,
	Example: `  multica view create --name "Open bugs" --label Bug --status done --status cancelled --negate status
  multica view create --name "My review queue" --scope my --scope-variant assigned --status in_review --view-mode list
  multica view create --name "Launch" --project "Website" --view-mode table --sort due_date --visibility workspace`,
	Args: exactArgs(0),
	RunE: runViewCreate,
}

var viewUpdateCmd = &cobra.Command{
	Use:   "update <view>",
	Short: "Change a saved view",
	Long: `Change a saved view. <view> is a view ID, ID prefix, or exact name
(case-insensitive). Only what a flag names changes; every other filter and
display setting stays as saved. A list flag replaces that filter's values;
pass it an empty value (--status "") to clear the filter.

--clear-filters drops every saved filter before the filter flags apply, which
is also how to replace a view's advanced condition tree with simple filters.

The write carries the revision that was read, so if the view changed in the
meantime the server refuses it instead of overwriting; re-run to apply the
change on top of the latest version. A --definition-file exported from this
view with ` + "`view get`" + ` carries its own revision: if the view changed after the
export, nothing is saved, so export it again and redo the edits. Only the owner can change a private view;
a workspace-shared view can also be changed by workspace owners and admins.`,
	Example: `  multica view update "Open bugs" --priority urgent --priority high
  multica view update "Open bugs" --status "" --view-mode board --grouping assignee
  multica view update 6a1b2c3d --visibility workspace --name "Team bugs"`,
	Args: exactArgs(1),
	RunE: runViewUpdate,
}

var viewDuplicateCmd = &cobra.Command{
	Use:   "duplicate <view>",
	Short: "Copy a saved view into a new view of your own",
	Long: `Copy a saved view into a new private view you own, in the same scope. Any
filter or display flag changes the copy, the same way it changes a view with
` + "`multica view update`" + `; the original is left as it is.`,
	Example: `  multica view duplicate "Open bugs" --name "Open bugs (urgent)" --priority urgent`,
	Args:    exactArgs(1),
	RunE:    runViewDuplicate,
}

var viewDeleteCmd = &cobra.Command{
	Use:   "delete <view>",
	Short: "Delete a saved view",
	Long: `Delete a saved view. <view> is a view ID, ID prefix, or exact name
(case-insensitive). Only the owner can delete a private view; a
workspace-shared view can also be deleted by workspace owners and admins.`,
	Args: exactArgs(1),
	RunE: runViewDelete,
}

func init() {
	viewCmd.AddCommand(viewCreateCmd)
	viewCmd.AddCommand(viewUpdateCmd)
	viewCmd.AddCommand(viewDuplicateCmd)
	viewCmd.AddCommand(viewDeleteCmd)

	viewCreateCmd.Flags().String("name", "", "View name (required, at most 80 characters)")
	viewCreateCmd.Flags().String("scope", "", "Page the view belongs to: workspace (All issues, the default), my (My issues), or project")
	viewCreateCmd.Flags().String("project", "", "Project the view belongs to (name, ID, or ID prefix); implies --scope project")
	viewCreateCmd.Flags().String("scope-variant", "", "Tab within the scope: all, members or agents for workspace and project views; any (default), assigned, created or involved for my views")
	viewCreateCmd.Flags().String("visibility", "private", "private, or workspace to share it with every member (my views are always private)")
	addViewDefinitionFlags(viewCreateCmd, false)
	viewCreateCmd.Flags().String("output", "json", "Output format: table or json")

	viewUpdateCmd.Flags().String("name", "", "New name (at most 80 characters)")
	viewUpdateCmd.Flags().String("scope-variant", "", "New tab within the view's scope (see `view create --help`); the scope itself cannot change")
	viewUpdateCmd.Flags().String("visibility", "", "private or workspace")
	addViewDefinitionFlags(viewUpdateCmd, true)
	viewUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	viewDuplicateCmd.Flags().String("name", "", `Name of the copy (default: "<name> (copy)")`)
	viewDuplicateCmd.Flags().String("visibility", "private", "private or workspace")
	addViewDefinitionFlags(viewDuplicateCmd, true)
	viewDuplicateCmd.Flags().String("output", "json", "Output format: table or json")

	viewDeleteCmd.Flags().String("output", "table", "Output format: table or json")
}

func addViewDefinitionFlags(cmd *cobra.Command, clearable bool) {
	f := cmd.Flags()
	f.StringArray("status", nil, "Status key or name to match")
	f.StringArray("priority", nil, "Priority to match: "+strings.Join(validIssuePriorities, ", "))
	f.StringArray("assignee", nil, "Assignee to match: member, agent or squad name, email, or ID")
	f.Bool("include-no-assignee", false, "Also match unassigned issues")
	f.StringArray("creator", nil, "Creator to match: member or agent name, email, or ID")
	f.StringArray("project-filter", nil, "Project to match (name, ID, or ID prefix); not available on project views")
	f.Bool("include-no-project", false, "Also match issues without a project; not available on project views")
	f.StringArray("project-status", nil, "Status of the issue's project to match: "+strings.Join(validProjectStatuses, ", "))
	f.StringArray("label", nil, "Label to match (name, ID, or ID prefix)")
	f.StringArray("relation", nil, "How the viewer relates to the issue: "+strings.Join(viewRelations, ", ")+"; not available on my views")
	f.StringArray("due", nil, "Due date bucket to match: "+strings.Join(viewDueDates, ", "))
	f.StringArray("property", nil, `Custom property condition, written as for `+"`issue list --property`"+`: "Name=Value", "Score>=3", "Notes~=review" (one per flag; repeating a property matches any of its values)`)
	f.StringArray("negate", nil, "Filter that matches \"is not\" instead of \"is\": "+strings.Join(viewNegatableFlagNames(), ", "))
	f.Bool("agent-running", false, "Only issues an agent is working on right now")
	f.Bool("agent-idle", false, "Only issues without running or queued agent work")
	f.Bool("unread", false, "Only issues with an agent result the viewer has not seen")

	f.String("view-mode", "", "Layout: "+strings.Join(viewModes, ", "))
	f.String("grouping", "", "Board columns: status, assignee, project, or property:<name> for a select property")
	f.String("table-grouping", "", "Table groups: none, status, assignee, project, or property:<name> for a select or checkbox property")
	f.String("swimlane-grouping", "", "Swimlanes: "+strings.Join(viewSwimlaneGroupings, ", "))
	f.String("gantt-zoom", "", "Gantt zoom: "+strings.Join(viewGanttZooms, ", "))
	f.String("sort", "", "Sort: "+strings.Join(viewSortFields, ", ")+", or property:<name>; position (manual order) needs status grouping")
	f.String("direction", "", "Sort direction: asc or desc (default: desc for created_at and updated_at, asc otherwise)")
	f.Bool("show-sub-issues", true, "Show sub-issues (--show-sub-issues=false lists top-level issues only)")

	f.String("definition-file", "", `JSON file with "query" and/or "display" objects (e.g. edited `+"`view get`"+` output); flags apply on top`)
	if clearable {
		f.Bool("clear-filters", false, "Drop every saved filter before the filter flags apply")
	}
	f.Bool("dry-run", false, "Print the request instead of saving it")
}

func viewNegatableFlagNames() []string {
	names := make([]string, len(viewNegatableDimensions))
	for i, d := range viewNegatableDimensions {
		names[i] = d.flag
	}
	return names
}

// issueViewDefinition is a view's query and display as generic JSON, so keys
// this CLI does not know round-trip untouched.
type issueViewDefinition struct {
	Query   map[string]any
	Display map[string]any
}

func copyJSONObject(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// definitionOfView returns a copy of a stored view's definition, refusing a
// format this CLI would corrupt by rewriting.
func definitionOfView(view map[string]any) (issueViewDefinition, error) {
	version, _ := view["definition_version"].(float64)
	if version != issueViewDefinitionVersion {
		return issueViewDefinition{}, fmt.Errorf("view %q is stored as definition_version %v, but this CLI writes version %d; update with `multica update`", strVal(view, "name"), view["definition_version"], issueViewDefinitionVersion)
	}
	query, err := copyJSONObject(view["query"])
	if err != nil {
		return issueViewDefinition{}, fmt.Errorf("read view query: %w", err)
	}
	display, err := copyJSONObject(view["display"])
	if err != nil {
		return issueViewDefinition{}, fmt.Errorf("read view display: %w", err)
	}
	return issueViewDefinition{Query: query, Display: display}, nil
}

// viewDefinitionFile is a parsed --definition-file. Query or Display is nil
// when the file omits it, meaning "keep the current one". ID and Revision are
// set when the file is a view exported with `view get`.
type viewDefinitionFile struct {
	Query, Display map[string]any
	ID             string
	Revision       float64
}

// loadViewDefinitionFile reads --definition-file, or returns nil when unset.
func loadViewDefinitionFile(cmd *cobra.Command) (*viewDefinitionFile, error) {
	path, _ := cmd.Flags().GetString("definition-file")
	if path == "" {
		return nil, nil
	}
	return readViewDefinitionFile(path)
}

// checkExportedRevision refuses a file exported from this view at an older
// revision: writing its query/display would undo whatever changed since.
// A file without a revision, or exported from another view, is a template.
func checkExportedRevision(file *viewDefinitionFile, view map[string]any) error {
	if file == nil || file.Revision == 0 || (file.ID != "" && file.ID != strVal(view, "id")) {
		return nil
	}
	if current, _ := view["revision"].(float64); file.Revision != current {
		return fmt.Errorf("--definition-file was exported from view %q at revision %v, but the view is now at revision %v; nothing was saved. Export it again with `multica view get` and redo your edits", strVal(view, "name"), file.Revision, view["revision"])
	}
	return nil
}

func readViewDefinitionFile(path string) (*viewDefinitionFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --definition-file: %w", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, fmt.Errorf("--definition-file must hold a JSON object with \"query\" and/or \"display\"")
	}
	if raw, ok := doc["definition_version"]; ok {
		var version float64
		if json.Unmarshal(raw, &version) != nil || version != issueViewDefinitionVersion {
			return nil, fmt.Errorf("--definition-file has definition_version %s, but this CLI writes version %d", string(raw), issueViewDefinitionVersion)
		}
	}
	file := &viewDefinitionFile{}
	if raw, ok := doc["id"]; ok {
		if json.Unmarshal(raw, &file.ID) != nil {
			return nil, fmt.Errorf("--definition-file: \"id\" must be a string")
		}
	}
	if raw, ok := doc["revision"]; ok {
		if json.Unmarshal(raw, &file.Revision) != nil || file.Revision < 1 {
			return nil, fmt.Errorf("--definition-file: \"revision\" must be a positive number")
		}
	}
	decode := func(key string) (map[string]any, error) {
		raw, ok := doc[key]
		if !ok {
			return nil, nil
		}
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil || object == nil {
			return nil, fmt.Errorf("--definition-file: %q must be a JSON object", key)
		}
		return object, nil
	}
	if file.Query, err = decode("query"); err != nil {
		return nil, err
	}
	if file.Display, err = decode("display"); err != nil {
		return nil, err
	}
	if file.Query == nil && file.Display == nil {
		return nil, fmt.Errorf("--definition-file has neither \"query\" nor \"display\"")
	}
	return file, nil
}

func anyFlagChanged(cmd *cobra.Command, names []string) bool {
	for _, name := range names {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
			return true
		}
	}
	return false
}

// viewDefinitionChanged reports whether any flag touches the definition.
func viewDefinitionChanged(cmd *cobra.Command) bool {
	return anyFlagChanged(cmd, viewFilterFlags) || anyFlagChanged(cmd, viewDisplayFlags) ||
		anyFlagChanged(cmd, []string{"definition-file", "clear-filters"})
}

// viewFlagValues reads a repeatable list flag, splitting comma-separated
// values. An empty value yields an empty list, which clears the filter.
func viewFlagValues(cmd *cobra.Command, name string) []string {
	raw, _ := cmd.Flags().GetStringArray(name)
	values := []string{}
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			if part = strings.TrimSpace(part); part != "" && !slices.Contains(values, part) {
				values = append(values, part)
			}
		}
	}
	return values
}

func checkViewEnum(flag string, values []string, allowed []string) error {
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return fmt.Errorf("invalid --%s %q; valid values: %s", flag, value, strings.Join(allowed, ", "))
		}
	}
	return nil
}

// matchNamedID resolves a ref against candidates by full ID, exact name
// (case-insensitive), then ID prefix.
func matchNamedID(kind, ref string, candidates []idCandidate) (idCandidate, error) {
	ref = strings.TrimSpace(ref)
	var named []idCandidate
	for _, c := range candidates {
		if c.ID == "" {
			continue
		}
		if strings.EqualFold(c.ID, ref) {
			return c, nil
		}
		if strings.EqualFold(strings.TrimSpace(c.Display), ref) {
			named = append(named, c)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
	default:
		return idCandidate{}, fmt.Errorf("more than one %s is named %q; pass its ID:\n%s", kind, ref, joinCandidateIDs(named))
	}
	prefix, err := normalizeUUIDPrefix(ref)
	if err != nil {
		return idCandidate{}, fmt.Errorf("no %s named %q", kind, ref)
	}
	var matches []idCandidate
	for _, c := range candidates {
		if c.ID != "" && strings.HasPrefix(compactUUID(c.ID), prefix) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return idCandidate{}, fmt.Errorf("no %s named or matching ID prefix %q", kind, ref)
	case 1:
		return matches[0], nil
	default:
		return idCandidate{}, ambiguousIDPrefixError(kind, ref, matches)
	}
}

func resolveNamedProject(ctx context.Context, client *cli.APIClient, ref string) (idCandidate, error) {
	candidates, err := fetchProjectCandidates(ctx, client)
	if err != nil {
		return idCandidate{}, fmt.Errorf("resolve project: %w", err)
	}
	return matchNamedID("project", ref, candidates)
}

// viewDefinitionBuilder applies the filter and display flags to a definition,
// fetching each catalog it needs at most once.
type viewDefinitionBuilder struct {
	ctx        context.Context
	client     *cli.APIClient
	cmd        *cobra.Command
	properties []propertyDTO
	loaded     bool
	members    memberDirectory
}

func (b *viewDefinitionBuilder) propertyCatalog() ([]propertyDTO, error) {
	if !b.loaded {
		properties, err := fetchProperties(b.ctx, b.client)
		if err != nil {
			return nil, err
		}
		b.properties, b.loaded = properties, true
	}
	return b.properties, nil
}

// propertyKey resolves "property:<name-or-id>" to "property:<id>" for a
// property of one of the given types.
func (b *viewDefinitionBuilder) propertyKey(flag, value string, types []string) (string, error) {
	ref := strings.TrimPrefix(value, "property:")
	properties, err := b.propertyCatalog()
	if err != nil {
		return "", err
	}
	property, err := resolvePropertyRef(properties, ref)
	if err != nil {
		return "", err
	}
	if property.Archived {
		return "", fmt.Errorf("--%s: property %q is archived; restore it first with `multica property unarchive`", flag, property.Name)
	}
	if !slices.Contains(types, property.Type) {
		return "", fmt.Errorf("--%s: %s property %q cannot be used here; allowed property types: %s", flag, property.Type, property.Name, strings.Join(types, ", "))
	}
	return "property:" + property.ID, nil
}

func (b *viewDefinitionBuilder) actors(flag string, kinds assigneeKinds) ([]any, error) {
	refs := viewFlagValues(b.cmd, flag)
	actors := make([]any, 0, len(refs))
	for _, ref := range refs {
		actorType, id, err := resolveAssignee(b.ctx, b.client, ref, kinds)
		if err != nil {
			return nil, fmt.Errorf("--%s: %w", flag, err)
		}
		actors = append(actors, map[string]any{"type": actorType, "id": id})
	}
	return actors, nil
}

func (b *viewDefinitionBuilder) changed(name string) bool {
	flag := b.cmd.Flags().Lookup(name)
	return flag != nil && flag.Changed
}

func (b *viewDefinitionBuilder) applyFilters(query map[string]any, scopeType string) error {
	cmd := b.cmd
	if anyFlagChanged(cmd, viewFlatFilterFlags) {
		if tree, ok := query["advancedFilter"]; ok && tree != nil {
			return fmt.Errorf("this view filters with an advanced condition tree, which the filter flags cannot edit; add --clear-filters to replace its filters, or edit query.advancedFilter through --definition-file")
		}
	}
	if scopeType == "project" && anyFlagChanged(cmd, []string{"project-filter", "include-no-project"}) {
		return fmt.Errorf("--project-filter and --include-no-project do not apply to a project view, which always shows its own project")
	}
	if scopeType == "my" && b.changed("relation") {
		return fmt.Errorf("--relation does not apply to a my view; its scope variant sets how you relate to the issues")
	}

	if b.changed("status") {
		refs := viewFlagValues(cmd, "status")
		keys := make([]any, 0, len(refs))
		if len(refs) > 0 {
			statuses, err := fetchIssueStatuses(b.ctx, b.client, true)
			if err != nil {
				return err
			}
			for _, ref := range refs {
				status, err := resolveIssueStatusRef(statuses, ref)
				if err != nil {
					return fmt.Errorf("--status: %w", err)
				}
				keys = append(keys, status.Key)
			}
		}
		query["statusFilters"] = keys
	}
	enumLists := []struct {
		flag, key string
		allowed   []string
	}{
		{"priority", "priorityFilters", validIssuePriorities},
		{"project-status", "projectStatusFilters", validProjectStatuses},
		{"relation", "relationFilters", viewRelations},
		{"due", "dueDateFilters", viewDueDates},
	}
	for _, list := range enumLists {
		if !b.changed(list.flag) {
			continue
		}
		values := viewFlagValues(cmd, list.flag)
		if err := checkViewEnum(list.flag, values, list.allowed); err != nil {
			return err
		}
		query[list.key] = values
	}
	if b.changed("assignee") {
		actors, err := b.actors("assignee", issueAssigneeKinds)
		if err != nil {
			return err
		}
		query["assigneeFilters"] = actors
	}
	if b.changed("creator") {
		actors, err := b.actors("creator", memberOrAgentKinds)
		if err != nil {
			return err
		}
		query["creatorFilters"] = actors
	}
	namedLists := []struct {
		flag, key, kind string
		fetch           func(context.Context, *cli.APIClient) ([]idCandidate, error)
	}{
		{"project-filter", "projectFilters", "project", fetchProjectCandidates},
		{"label", "labelFilters", "label", func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
			return fetchLabelCandidates(ctx, client, "issue")
		}},
	}
	for _, list := range namedLists {
		if !b.changed(list.flag) {
			continue
		}
		refs := viewFlagValues(cmd, list.flag)
		ids := make([]any, 0, len(refs))
		if len(refs) > 0 {
			candidates, err := list.fetch(b.ctx, b.client)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", list.kind, err)
			}
			for _, ref := range refs {
				match, err := matchNamedID(list.kind, ref, candidates)
				if err != nil {
					return fmt.Errorf("--%s: %w", list.flag, err)
				}
				ids = append(ids, match.ID)
			}
		}
		query[list.key] = ids
	}
	if b.changed("property") {
		raw, _ := cmd.Flags().GetStringArray("property")
		pairs := make([]string, 0, len(raw))
		for _, pair := range raw {
			if strings.TrimSpace(pair) != "" {
				pairs = append(pairs, pair)
			}
		}
		filters := map[string]any{}
		if len(pairs) > 0 {
			properties, err := b.propertyCatalog()
			if err != nil {
				return err
			}
			encoded, err := buildPropertiesFilterQueryParam(b.ctx, b.client, &b.members, properties, pairs)
			if err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(encoded), &filters); err != nil {
				return fmt.Errorf("encode --property: %w", err)
			}
		}
		query["propertyFilters"] = filters
	}
	if b.changed("negate") {
		values := viewFlagValues(cmd, "negate")
		if err := checkViewEnum("negate", values, viewNegatableFlagNames()); err != nil {
			return err
		}
		dimensions := []any{}
		for _, d := range viewNegatableDimensions {
			if slices.Contains(values, d.flag) {
				dimensions = append(dimensions, d.dimension)
			}
		}
		query["negatedFilters"] = dimensions
	}
	for _, flag := range []struct{ name, key string }{
		{"include-no-assignee", "includeNoAssignee"},
		{"include-no-project", "includeNoProject"},
		{"unread", "unreadFilter"},
	} {
		if b.changed(flag.name) {
			value, _ := cmd.Flags().GetBool(flag.name)
			query[flag.key] = value
		}
	}
	running, _ := cmd.Flags().GetBool("agent-running")
	idle, _ := cmd.Flags().GetBool("agent-idle")
	if running && idle {
		return fmt.Errorf("--agent-running and --agent-idle cannot both be set")
	}
	if b.changed("agent-running") {
		query["agentRunningFilter"] = running
		if running {
			query["agentIdleFilter"] = false
		}
	}
	if b.changed("agent-idle") {
		query["agentIdleFilter"] = idle
		if idle {
			query["agentRunningFilter"] = false
		}
	}
	return nil
}

func (b *viewDefinitionBuilder) applyDisplay(display map[string]any) error {
	cmd := b.cmd
	enums := []struct {
		flag, key string
		allowed   []string
		// propertyTypes, when set, also allows property:<name> of these types.
		propertyTypes []string
	}{
		{"view-mode", "viewMode", viewModes, nil},
		{"grouping", "grouping", viewGroupings, []string{"select"}},
		{"table-grouping", "tableGrouping", viewTableGroupings, []string{"select", "checkbox"}},
		{"swimlane-grouping", "swimlaneGrouping", viewSwimlaneGroupings, nil},
		{"gantt-zoom", "ganttZoom", viewGanttZooms, nil},
	}
	for _, e := range enums {
		if !b.changed(e.flag) {
			continue
		}
		value, _ := cmd.Flags().GetString(e.flag)
		value = strings.TrimSpace(value)
		if e.propertyTypes != nil && strings.HasPrefix(value, "property:") {
			key, err := b.propertyKey(e.flag, value, e.propertyTypes)
			if err != nil {
				return err
			}
			display[e.key] = key
			continue
		}
		if err := checkViewEnum(e.flag, []string{value}, e.allowed); err != nil {
			return err
		}
		display[e.key] = value
	}

	if b.changed("sort") {
		value, _ := cmd.Flags().GetString("sort")
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "property:") {
			properties, err := b.propertyCatalog()
			if err != nil {
				return err
			}
			property, err := resolveSortableProperty(properties, strings.TrimPrefix(value, "property:"))
			if err != nil {
				return err
			}
			value = "property:" + property.ID
		} else if err := checkViewEnum("sort", []string{value}, viewSortFields); err != nil {
			return err
		}
		display["sortBy"] = value
		if !b.changed("direction") {
			direction := "asc"
			if value == "created_at" || value == "updated_at" {
				direction = "desc"
			}
			display["sortDirection"] = direction
		}
	}
	if b.changed("direction") {
		value, _ := cmd.Flags().GetString("direction")
		value = strings.ToLower(strings.TrimSpace(value))
		if err := checkViewEnum("direction", []string{value}, []string{"asc", "desc"}); err != nil {
			return err
		}
		display["sortDirection"] = value
	}
	if b.changed("show-sub-issues") {
		value, _ := cmd.Flags().GetBool("show-sub-issues")
		display["showSubIssues"] = value
	}

	// Manual order is one shared position sequence per status column, so the
	// client only honours it under status grouping.
	if anyFlagChanged(cmd, []string{"sort", "grouping"}) {
		grouping, ok := display["grouping"].(string)
		if !ok {
			grouping = "status"
		}
		if sortBy, _ := display["sortBy"].(string); sortBy == "position" && grouping != "status" {
			return fmt.Errorf("--sort position (manual order) needs status grouping, but the view groups by %s", grouping)
		}
	}
	return nil
}

// buildViewDefinition layers file (--definition-file, may be nil),
// --clear-filters and the filter and display flags over base, in that order.
func buildViewDefinition(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, base issueViewDefinition, file *viewDefinitionFile, scopeType string) (issueViewDefinition, error) {
	def := base
	if file != nil {
		if file.Query != nil {
			def.Query = file.Query
		}
		if file.Display != nil {
			def.Display = file.Display
		}
	}
	if clear, _ := cmd.Flags().GetBool("clear-filters"); clear {
		def.Query = map[string]any{}
	}
	builder := &viewDefinitionBuilder{ctx: ctx, client: client, cmd: cmd}
	if err := builder.applyFilters(def.Query, scopeType); err != nil {
		return def, err
	}
	if err := builder.applyDisplay(def.Display); err != nil {
		return def, err
	}
	return def, nil
}

func validateViewName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > issueViewNameMaxLen {
		return "", fmt.Errorf("view name must be between 1 and %d characters", issueViewNameMaxLen)
	}
	return name, nil
}

func validateViewScopeVariant(scopeType, variant string) error {
	allowed := viewScopeVariants[scopeType]
	if !slices.Contains(allowed, variant) {
		return fmt.Errorf("invalid --scope-variant %q for a %s view; valid values: %s", variant, scopeType, strings.Join(allowed, ", "))
	}
	return nil
}

func validateViewVisibility(scopeType, visibility string) error {
	if err := checkViewEnum("visibility", []string{visibility}, viewVisibilities); err != nil {
		return err
	}
	if scopeType == "my" && visibility != "private" {
		return fmt.Errorf("my views are always private")
	}
	return nil
}

func printViewDryRun(method, path string, body map[string]any) error {
	fmt.Fprintf(os.Stderr, "Dry run: %s %s was not sent.\n", method, path)
	return cli.PrintJSON(os.Stdout, body)
}

func printWrittenView(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, view map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		cli.PrintTable(os.Stdout, []string{"ID", "NAME", "SCOPE", "VISIBILITY", "OWNER", "REVISION"}, [][]string{{
			strVal(view, "id"),
			strVal(view, "name"),
			viewScopeLabel(view, true),
			strVal(view, "visibility"),
			actors.actor("member", strVal(view, "owner_id")),
			fmt.Sprintf("%v", view["revision"]),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, view)
}

// viewWriteError turns the server's permission and revision refusals into
// guidance; anything else passes through.
func viewWriteError(action string, view map[string]any, err error) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusForbidden:
			return fmt.Errorf("%s view %q: only the owner can change a private view, and only the owner or a workspace owner/admin a shared one", action, strVal(view, "name"))
		case http.StatusConflict:
			return fmt.Errorf("%s view %q: it changed since it was read (revision %v), so nothing was saved; re-run to apply your change to the latest version", action, strVal(view, "name"), view["revision"])
		}
	}
	return fmt.Errorf("%s view: %w", action, err)
}

// starViewInSidebar adds a view to the user's sidebar the way the app does
// after creating one. A sidebar the user never arranged already lists their
// own views, so it is only written once it has a saved order.
func starViewInSidebar(ctx context.Context, client *cli.APIClient, viewID string) error {
	var pref struct {
		Prefs     map[string]any `json:"prefs"`
		UpdatedAt string         `json:"updated_at"`
	}
	if err := client.GetJSON(ctx, "/api/issue-view-preferences?"+url.Values{"scope_type": {"sidebar"}}.Encode(), &pref); err != nil {
		return err
	}
	if pref.UpdatedAt == "" {
		return nil
	}
	if pref.Prefs == nil {
		pref.Prefs = map[string]any{}
	}
	item := "view:" + viewID
	order, _ := pref.Prefs["order"].([]any)
	if slices.Contains(order, any(item)) {
		return nil
	}
	pref.Prefs["order"] = append(order, item)
	if _, ok := pref.Prefs["hidden"]; !ok {
		pref.Prefs["hidden"] = []any{}
	}
	return client.PutJSON(ctx, "/api/issue-view-preferences", map[string]any{
		"scope_type": "sidebar",
		"prefs":      pref.Prefs,
	}, nil)
}

func createIssueViewAndStar(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, body map[string]any) error {
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		return printViewDryRun(http.MethodPost, "/api/issue-views", body)
	}
	var created map[string]any
	if err := client.PostJSON(ctx, "/api/issue-views", body, &created); err != nil {
		return fmt.Errorf("create view: %w", err)
	}
	if err := starViewInSidebar(ctx, client, strVal(created, "id")); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: view created, but adding it to your sidebar failed: %v\n", err)
	}
	return printWrittenView(ctx, cmd, client, created)
}

func runViewCreate(cmd *cobra.Command, _ []string) error {
	rawName, _ := cmd.Flags().GetString("name")
	name, err := validateViewName(rawName)
	if err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required to create a view")
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	scope, _ := cmd.Flags().GetString("scope")
	projectRef, _ := cmd.Flags().GetString("project")
	if scope == "" {
		scope = "workspace"
		if projectRef != "" {
			scope = "project"
		}
	}
	if _, ok := viewScopeVariants[scope]; !ok {
		return fmt.Errorf("invalid --scope %q; valid values: workspace, my, project", scope)
	}
	body := map[string]any{
		"name":               name,
		"scope_type":         scope,
		"definition_version": issueViewDefinitionVersion,
	}
	switch {
	case scope == "project" && projectRef == "":
		return fmt.Errorf("--project is required for a project view")
	case scope != "project" && projectRef != "":
		return fmt.Errorf("--project names the project of a project view; it cannot be combined with --scope %s (to filter by project, use --project-filter)", scope)
	case scope == "project":
		project, err := resolveNamedProject(ctx, client, projectRef)
		if err != nil {
			return fmt.Errorf("--project: %w", err)
		}
		body["scope_id"] = project.ID
	}
	variant, _ := cmd.Flags().GetString("scope-variant")
	if variant == "" {
		variant = viewScopeVariants[scope][0]
	}
	if err := validateViewScopeVariant(scope, variant); err != nil {
		return err
	}
	body["scope_variant"] = variant
	visibility, _ := cmd.Flags().GetString("visibility")
	if scope == "my" && !cmd.Flags().Changed("visibility") {
		visibility = "private"
	}
	if err := validateViewVisibility(scope, visibility); err != nil {
		return err
	}
	body["visibility"] = visibility

	file, err := loadViewDefinitionFile(cmd)
	if err != nil {
		return err
	}
	def, err := buildViewDefinition(ctx, client, cmd, issueViewDefinition{Query: map[string]any{}, Display: map[string]any{}}, file, scope)
	if err != nil {
		return err
	}
	body["query"], body["display"] = def.Query, def.Display
	return createIssueViewAndStar(ctx, cmd, client, body)
}

func runViewDuplicate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	source, err := resolveIssueView(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve view: %w", err)
	}
	base, err := definitionOfView(source)
	if err != nil {
		return err
	}
	scope := strVal(source, "scope_type")
	name, _ := cmd.Flags().GetString("name")
	if !cmd.Flags().Changed("name") {
		name = strVal(source, "name") + " (copy)"
		if runes := []rune(name); len(runes) > issueViewNameMaxLen {
			name = string(runes[:issueViewNameMaxLen])
		}
	}
	if name, err = validateViewName(name); err != nil {
		return fmt.Errorf("--name: %w", err)
	}
	visibility, _ := cmd.Flags().GetString("visibility")
	if err := validateViewVisibility(scope, visibility); err != nil {
		return err
	}
	body := map[string]any{
		"name":               name,
		"scope_type":         scope,
		"scope_variant":      source["scope_variant"],
		"visibility":         visibility,
		"definition_version": issueViewDefinitionVersion,
	}
	if scope == "project" {
		body["scope_id"] = source["scope_id"]
	}
	file, err := loadViewDefinitionFile(cmd)
	if err != nil {
		return err
	}
	def, err := buildViewDefinition(ctx, client, cmd, base, file, scope)
	if err != nil {
		return err
	}
	body["query"], body["display"] = def.Query, def.Display
	return createIssueViewAndStar(ctx, cmd, client, body)
}

func runViewUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	view, err := resolveIssueView(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve view: %w", err)
	}
	scope := strVal(view, "scope_type")
	patch := map[string]any{"expected_revision": view["revision"]}
	if cmd.Flags().Changed("name") {
		rawName, _ := cmd.Flags().GetString("name")
		name, err := validateViewName(rawName)
		if err != nil {
			return fmt.Errorf("--name: %w", err)
		}
		patch["name"] = name
	}
	if cmd.Flags().Changed("visibility") {
		visibility, _ := cmd.Flags().GetString("visibility")
		if err := validateViewVisibility(scope, visibility); err != nil {
			return err
		}
		patch["visibility"] = visibility
	}
	if cmd.Flags().Changed("scope-variant") {
		variant, _ := cmd.Flags().GetString("scope-variant")
		if err := validateViewScopeVariant(scope, variant); err != nil {
			return err
		}
		patch["scope_variant"] = variant
	}
	if viewDefinitionChanged(cmd) {
		base, err := definitionOfView(view)
		if err != nil {
			return err
		}
		file, err := loadViewDefinitionFile(cmd)
		if err != nil {
			return err
		}
		// After this check the file's revision is the one sent as
		// expected_revision, so a change landing before the write is a 409.
		if err := checkExportedRevision(file, view); err != nil {
			return err
		}
		def, err := buildViewDefinition(ctx, client, cmd, base, file, scope)
		if err != nil {
			return err
		}
		patch["query"], patch["display"] = def.Query, def.Display
		patch["definition_version"] = issueViewDefinitionVersion
	}
	if len(patch) == 1 {
		return fmt.Errorf("nothing to update; pass at least one flag (see `multica view update --help`)")
	}

	path := "/api/issue-views/" + url.PathEscape(strVal(view, "id"))
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		return printViewDryRun(http.MethodPatch, path, patch)
	}
	var updated map[string]any
	if err := client.PatchJSON(ctx, path, patch, &updated); err != nil {
		return viewWriteError("update", view, err)
	}
	return printWrittenView(ctx, cmd, client, updated)
}

func runViewDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	view, err := resolveIssueView(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve view: %w", err)
	}
	id := strVal(view, "id")
	if err := client.DeleteJSON(ctx, "/api/issue-views/"+url.PathEscape(id)); err != nil {
		return viewWriteError("delete", view, err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": id, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "View %q deleted.\n", strVal(view, "name"))
	return nil
}
