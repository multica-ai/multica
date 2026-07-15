package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var viewCmd = &cobra.Command{
	Use:   "view",
	Short: "Work with saved issue views",
}

var viewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved issue views for a surface",
	RunE:  runViewList,
}

var viewGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a saved issue view",
	Args:  exactArgs(1),
	RunE:  runViewGet,
}

var viewIssuesCmd = &cobra.Command{
	Use:   "issues <view-id>",
	Short: "List issues matched by a saved view",
	Args:  exactArgs(1),
	RunE:  runViewIssues,
}

type savedViewActorFilter struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type savedViewDateFilter struct {
	Field  string `json:"field"`
	From   string `json:"from"`
	To     string `json:"to"`
	Preset string `json:"preset,omitempty"`
}

type savedIssueViewDefinition struct {
	Version            int                    `json:"version"`
	ViewMode           string                 `json:"viewMode"`
	Grouping           string                 `json:"grouping"`
	StatusFilters      []string               `json:"statusFilters"`
	PriorityFilters    []string               `json:"priorityFilters"`
	AssigneeFilters    []savedViewActorFilter `json:"assigneeFilters"`
	IncludeNoAssignee  bool                   `json:"includeNoAssignee"`
	CreatorFilters     []savedViewActorFilter `json:"creatorFilters"`
	ProjectFilters     []string               `json:"projectFilters"`
	IncludeNoProject   bool                   `json:"includeNoProject"`
	LabelFilters       []string               `json:"labelFilters"`
	PropertyFilters    map[string][]string    `json:"propertyFilters"`
	DateFilter         *savedViewDateFilter   `json:"dateFilter"`
	AgentRunningFilter bool                   `json:"agentRunningFilter"`
	SortBy             string                 `json:"sortBy"`
	SortDirection      string                 `json:"sortDirection"`
	CardProperties     map[string]bool        `json:"cardProperties"`
	CardPropertyIDs    []string               `json:"cardPropertyIds"`
	ShowSubIssues      *bool                  `json:"showSubIssues,omitempty"`
	ListCollapsed      []string               `json:"listCollapsedStatuses"`
	GanttZoom          string                 `json:"ganttZoom"`
	GanttShowCompleted bool                   `json:"ganttShowCompleted"`
	SwimlaneGrouping   string                 `json:"swimlaneGrouping"`
	SwimlaneOrders     map[string][]string    `json:"swimlaneOrders"`
	CollapsedSwimlanes map[string][]string    `json:"collapsedSwimlanes"`
	WorkspaceActorKind string                 `json:"workspaceActorKind,omitempty"`
	MyRelation         string                 `json:"myRelation,omitempty"`
}

type savedIssueView struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	ScopeType  string                   `json:"scope_type"`
	ScopeID    *string                  `json:"scope_id"`
	Visibility string                   `json:"visibility"`
	Definition savedIssueViewDefinition `json:"definition"`
	UpdatedAt  string                   `json:"updated_at"`
}

type groupedViewIssuesResponse struct {
	Groups []struct {
		ID     string           `json:"id"`
		Issues []map[string]any `json:"issues"`
		Total  int              `json:"total"`
	} `json:"groups"`
}

const (
	savedViewIssuePageSize = 100
	maxSavedViewIssuePages = 1000
)

func init() {
	viewCmd.AddCommand(viewListCmd)
	viewCmd.AddCommand(viewGetCmd)
	viewCmd.AddCommand(viewIssuesCmd)

	viewListCmd.Flags().String("scope", "workspace", "Surface scope: workspace, project, or my")
	viewListCmd.Flags().String("scope-id", "", "Project UUID (required with --scope project)")
	viewListCmd.Flags().String("output", "table", "Output format: table or json")
	viewListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	viewGetCmd.Flags().String("output", "json", "Output format: table or json")

	viewIssuesCmd.Flags().Int("limit", 50, "Maximum number of matched issues to return (max 100)")
	viewIssuesCmd.Flags().Int("offset", 0, "Number of matched issues to skip")
	viewIssuesCmd.Flags().String("output", "table", "Output format: table or json")
	viewIssuesCmd.Flags().Bool("full-id", false, "Show full issue UUIDs in table output")
}

func runViewList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	scope, _ := cmd.Flags().GetString("scope")
	if scope != "workspace" && scope != "project" && scope != "my" {
		return fmt.Errorf("invalid --scope %q; valid values: workspace, project, my", scope)
	}
	params := url.Values{"scope_type": []string{scope}}
	if scopeID, _ := cmd.Flags().GetString("scope-id"); scopeID != "" {
		params.Set("scope_id", scopeID)
	} else if scope == "project" {
		return fmt.Errorf("--scope-id is required with --scope project")
	}
	var result struct {
		Views         []savedIssueView `json:"views"`
		DefaultViewID *string          `json:"default_view_id"`
	}
	if err := client.GetJSON(ctx, "/api/views?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("list saved views: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	rows := make([][]string, 0, len(result.Views))
	for _, view := range result.Views {
		marker := ""
		if result.DefaultViewID != nil && *result.DefaultViewID == view.ID {
			marker = "default"
		}
		rows = append(rows, []string{
			displayID(view.ID, fullID), view.Name, view.ScopeType,
			view.Visibility, marker, view.UpdatedAt,
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "SCOPE", "VISIBILITY", "DEFAULT", "UPDATED"}, rows)
	return nil
}

func runViewGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var view savedIssueView
	if err := client.GetJSON(ctx, "/api/views/"+url.PathEscape(args[0]), &view); err != nil {
		return fmt.Errorf("get saved view: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "NAME", "SCOPE", "VISIBILITY"}, [][]string{{
			view.ID, view.Name, view.ScopeType, view.Visibility,
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, view)
}

func actorFilterParam(filters []savedViewActorFilter) string {
	values := make([]string, 0, len(filters))
	for _, filter := range filters {
		if filter.Type != "member" && filter.Type != "agent" && filter.Type != "squad" {
			continue
		}
		if strings.TrimSpace(filter.ID) != "" {
			values = append(values, filter.Type+":"+filter.ID)
		}
	}
	return strings.Join(values, ",")
}

func cloneURLValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

func fetchAllSavedViewIssues(
	ctx context.Context,
	client *cli.APIClient,
	params url.Values,
) ([]map[string]any, error) {
	params = cloneURLValues(params)
	params.Set("limit", strconv.Itoa(savedViewIssuePageSize))

	groupOrder := make([]string, 0)
	groupIssues := make(map[string][]map[string]any)
	groupTotals := make(map[string]int)
	completed := false
	for page := 0; page < maxSavedViewIssuePages; page++ {
		offset := page * savedViewIssuePageSize
		params.Set("offset", strconv.Itoa(offset))
		var response groupedViewIssuesResponse
		if err := client.GetJSON(ctx, "/api/issues/grouped?"+params.Encode(), &response); err != nil {
			return nil, err
		}

		pageCount := 0
		for _, group := range response.Groups {
			if _, exists := groupIssues[group.ID]; !exists {
				groupOrder = append(groupOrder, group.ID)
			}
			groupIssues[group.ID] = append(groupIssues[group.ID], group.Issues...)
			groupTotals[group.ID] = group.Total
			pageCount += len(group.Issues)
		}
		if pageCount == 0 {
			completed = true
			break
		}

		complete := true
		for _, groupID := range groupOrder {
			if len(groupIssues[groupID]) < groupTotals[groupID] {
				complete = false
				break
			}
		}
		if complete {
			completed = true
			break
		}
	}
	if !completed {
		return nil, fmt.Errorf("saved view exceeds the CLI pagination safety limit")
	}

	issues := make([]map[string]any, 0)
	for _, groupID := range groupOrder {
		issues = append(issues, groupIssues[groupID]...)
	}
	return issues, nil
}

func currentCLIUserID(ctx context.Context, client *cli.APIClient) (string, error) {
	var me map[string]any
	if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
		return "", err
	}
	id := strVal(me, "id")
	if id == "" {
		return "", fmt.Errorf("current user response has no id")
	}
	return id, nil
}

func issueParamsForSavedView(view savedIssueView) (url.Values, error) {
	definition := view.Definition
	if definition.Version < 1 {
		return nil, fmt.Errorf("saved view %s has an unsupported definition", view.ID)
	}
	params := url.Values{"group_by": []string{"assignee"}}
	if len(definition.StatusFilters) > 0 {
		params.Set("statuses", strings.Join(definition.StatusFilters, ","))
	}
	if len(definition.PriorityFilters) > 0 {
		params.Set("priorities", strings.Join(definition.PriorityFilters, ","))
	}
	if value := actorFilterParam(definition.AssigneeFilters); value != "" {
		params.Set("assignee_filters", value)
	}
	if definition.IncludeNoAssignee {
		params.Set("include_no_assignee", "true")
	}
	if value := actorFilterParam(definition.CreatorFilters); value != "" {
		params.Set("creator_filters", value)
	}
	if len(definition.ProjectFilters) > 0 {
		params.Set("project_ids", strings.Join(definition.ProjectFilters, ","))
	}
	if definition.IncludeNoProject {
		params.Set("include_no_project", "true")
	}
	if len(definition.LabelFilters) > 0 {
		params.Set("label_ids", strings.Join(definition.LabelFilters, ","))
	}
	if len(definition.PropertyFilters) > 0 {
		encoded, err := json.Marshal(definition.PropertyFilters)
		if err != nil {
			return nil, fmt.Errorf("encode property filters: %w", err)
		}
		params.Set("properties", string(encoded))
	}
	if definition.DateFilter != nil {
		from, to, err := savedViewDateBounds(definition.DateFilter, time.Now())
		if err != nil {
			return nil, err
		}
		params.Set("date_field", definition.DateFilter.Field)
		params.Set("date_start", from)
		params.Set("date_end", to)
	}
	if definition.SortBy != "" {
		params.Set("sort", definition.SortBy)
	}
	if definition.SortBy != "" && definition.SortBy != "position" && definition.SortDirection != "" {
		params.Set("direction", definition.SortDirection)
	}
	if view.ScopeType == "project" && view.ScopeID != nil {
		params.Set("project_id", *view.ScopeID)
	}
	if view.ScopeType == "workspace" {
		switch definition.WorkspaceActorKind {
		case "members":
			params.Set("assignee_types", "member")
		case "agents":
			params.Set("assignee_types", "agent,squad")
		}
	}
	return params, nil
}

func savedViewDateBounds(filter *savedViewDateFilter, now time.Time) (string, string, error) {
	if filter == nil || filter.Preset == "" {
		if filter == nil {
			return "", "", nil
		}
		return filter.From, filter.To, nil
	}

	days := 0
	switch filter.Preset {
	case "today":
		days = 1
	case "last_3_days":
		days = 3
	case "last_7_days":
		days = 7
	default:
		return "", "", fmt.Errorf("saved view has invalid date preset %q", filter.Preset)
	}
	return now.AddDate(0, 0, 1-days).Format("2006-01-02"), now.Format("2006-01-02"), nil
}

func deduplicateViewIssues(issues []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(issues))
	result := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		id := strVal(issue, "id")
		if _, exists := seen[id]; id == "" || exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, issue)
	}
	return result
}

func filterRunningViewIssues(
	ctx context.Context,
	client *cli.APIClient,
	issues []map[string]any,
) ([]map[string]any, error) {
	var tasks []map[string]any
	if err := client.GetJSON(ctx, "/api/agent-task-snapshot", &tasks); err != nil {
		return nil, err
	}
	running := make(map[string]struct{})
	for _, task := range tasks {
		if strVal(task, "status") == "running" {
			running[strVal(task, "issue_id")] = struct{}{}
		}
	}
	filtered := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		if _, ok := running[strVal(issue, "id")]; ok {
			filtered = append(filtered, issue)
		}
	}
	return filtered, nil
}

func runViewIssues(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	if limit < 1 || limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	if offset < 0 {
		return fmt.Errorf("--offset must be non-negative")
	}

	var view savedIssueView
	if err := client.GetJSON(ctx, "/api/views/"+url.PathEscape(args[0]), &view); err != nil {
		return fmt.Errorf("get saved view: %w", err)
	}
	baseParams, err := issueParamsForSavedView(view)
	if err != nil {
		return err
	}

	paramSets := []url.Values{baseParams}
	if view.ScopeType == "my" {
		userID, err := currentCLIUserID(ctx, client)
		if err != nil {
			return fmt.Errorf("resolve current user for My Issues view: %w", err)
		}
		relation := view.Definition.MyRelation
		if relation == "" {
			relation = "assigned"
		}
		makeRelationParams := func(key string) url.Values {
			params := cloneURLValues(baseParams)
			params.Set(key, userID)
			return params
		}
		switch relation {
		case "assigned":
			paramSets = []url.Values{makeRelationParams("assignee_id")}
		case "created":
			paramSets = []url.Values{makeRelationParams("creator_id")}
		case "involved":
			paramSets = []url.Values{makeRelationParams("involves_user_id")}
		case "all":
			paramSets = []url.Values{
				makeRelationParams("assignee_id"),
				makeRelationParams("creator_id"),
				makeRelationParams("involves_user_id"),
			}
		default:
			return fmt.Errorf("saved view has invalid My Issues relation %q", relation)
		}
	}

	issues := make([]map[string]any, 0)
	for _, params := range paramSets {
		page, err := fetchAllSavedViewIssues(ctx, client, params)
		if err != nil {
			return fmt.Errorf("list issues for saved view: %w", err)
		}
		issues = append(issues, page...)
	}
	issues = deduplicateViewIssues(issues)
	// showSubIssues predates saved views and defaults on in the UI. Treat a
	// missing field from an older/future partial definition the same way.
	if view.Definition.ShowSubIssues != nil && !*view.Definition.ShowSubIssues {
		filtered := issues[:0]
		for _, issue := range issues {
			if strVal(issue, "parent_issue_id") == "" {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}
	if view.Definition.AgentRunningFilter {
		issues, err = filterRunningViewIssues(ctx, client, issues)
		if err != nil {
			return fmt.Errorf("filter running issues: %w", err)
		}
	}

	totalFetched := len(issues)
	start := offset
	if start > totalFetched {
		start = totalFetched
	}
	end := start + limit
	if end > totalFetched {
		end = totalFetched
	}
	issues = issues[start:end]

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"view":     view,
			"issues":   issues,
			"count":    len(issues),
			"limit":    limit,
			"offset":   offset,
			"has_more": end < totalFetched,
		})
	}
	printSavedViewIssuesTable(ctx, client, issues, cmd)
	return nil
}

func printSavedViewIssuesTable(
	ctx context.Context,
	client *cli.APIClient,
	issues []map[string]any,
	cmd *cobra.Command,
) {
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	if fullID {
		headers = []string{"KEY", "ID", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "START DATE", "DUE DATE"}
	}
	actors := loadActorDisplayLookup(ctx, client)
	rows := make([][]string, 0, len(issues))
	for _, issue := range issues {
		startDate := strVal(issue, "start_date")
		if len(startDate) >= 10 {
			startDate = startDate[:10]
		}
		dueDate := strVal(issue, "due_date")
		if len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		row := []string{
			issueDisplayKey(issue), strVal(issue, "title"),
			strVal(issue, "status"), strVal(issue, "priority"),
			formatAssignee(issue, actors), startDate, dueDate,
		}
		if fullID {
			row = append([]string{issueDisplayKey(issue), strVal(issue, "id")}, row[1:]...)
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
