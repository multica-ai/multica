package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
)

type lifecycleFilePrincipal struct {
	Type string `json:"type" yaml:"type"`
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

type lifecycleFileEntryPolicy struct {
	Assignee     lifecycleFilePrincipal `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	Executor     lifecycleFilePrincipal `json:"executor,omitempty" yaml:"executor,omitempty"`
	Instructions string                 `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Advance      string                 `json:"advance,omitempty" yaml:"advance,omitempty"`
}

type lifecycleFileStatus struct {
	Key         string                   `json:"key" yaml:"key"`
	Name        string                   `json:"name" yaml:"name"`
	Description string                   `json:"description,omitempty" yaml:"description,omitempty"`
	Color       string                   `json:"color" yaml:"color"`
	Phase       string                   `json:"phase" yaml:"phase"`
	EntryPolicy lifecycleFileEntryPolicy `json:"entry_policy,omitempty" yaml:"entry_policy,omitempty"`
}

type lifecycleFileSpec struct {
	APIVersion    int                   `json:"api_version" yaml:"api_version"`
	Name          string                `json:"name" yaml:"name"`
	InitialStatus string                `json:"initial_status" yaml:"initial_status"`
	Statuses      []lifecycleFileStatus `json:"statuses" yaml:"statuses"`
}

type lifecycleAPISpec struct {
	APIVersion    int                  `json:"api_version"`
	Name          string               `json:"name"`
	InitialStatus string               `json:"initial_status"`
	Statuses      []lifecycleAPIStatus `json:"statuses"`
}

type lifecycleAPIStatus struct {
	Key         string                     `json:"key"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Color       string                     `json:"color"`
	Phase       string                     `json:"phase"`
	EntryPolicy issuelifecycle.EntryPolicy `json:"entry_policy"`
}

type lifecycleAPIResponse struct {
	Lifecycle struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Revision        int64   `json:"revision"`
		InitialStatusID *string `json:"initial_status_id"`
	} `json:"lifecycle"`
	Statuses []struct {
		ID          string                     `json:"id"`
		SpecKey     string                     `json:"spec_key"`
		Name        string                     `json:"name"`
		Description string                     `json:"description"`
		Color       string                     `json:"color"`
		Phase       string                     `json:"phase"`
		ArchivedAt  *string                    `json:"archived_at"`
		EntryPolicy issuelifecycle.EntryPolicy `json:"entry_policy"`
	} `json:"statuses"`
	Mode   string `json:"mode"`
	Plan   any    `json:"plan,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

var projectLifecycleCmd = &cobra.Command{Use: "lifecycle", Short: "Manage a project's issue lifecycle"}

var projectLifecycleGetCmd = &cobra.Command{
	Use: "get <project-id>", Short: "Export the effective lifecycle", Args: exactArgs(1), RunE: runProjectLifecycleGet,
}

var projectLifecycleApplyCmd = &cobra.Command{
	Use: "apply <project-id>", Short: "Apply a lifecycle YAML or JSON file", Args: exactArgs(1), RunE: runProjectLifecycleApply,
}

var projectLifecycleUseDefaultCmd = &cobra.Command{
	Use: "use-default <project-id>", Short: "Make a project inherit the workspace lifecycle", Args: exactArgs(1), RunE: runProjectLifecycleUseDefault,
}

var issueLifecycleStatusCmd = &cobra.Command{
	Use: "lifecycle-status <issue-id> <status>", Short: "Transition an issue by lifecycle status ID, stable key, or exact name", Args: exactArgs(2), RunE: runIssueLifecycleStatus,
}

func init() {
	projectCmd.AddCommand(projectLifecycleCmd)
	projectLifecycleCmd.AddCommand(projectLifecycleGetCmd, projectLifecycleApplyCmd, projectLifecycleUseDefaultCmd)
	projectLifecycleGetCmd.Flags().String("output", "yaml", "Output format: yaml or json")
	projectLifecycleGetCmd.Flags().Bool("include-archived", false, "Include archived status nodes")
	projectLifecycleApplyCmd.Flags().String("file", "", "Lifecycle YAML or JSON file (required)")
	projectLifecycleApplyCmd.Flags().Bool("allow-external-file", false, "Allow --file to read outside the current working directory")
	projectLifecycleApplyCmd.Flags().Int64("expected-revision", 0, "Only apply when the server lifecycle has this revision")
	projectLifecycleApplyCmd.Flags().Bool("allow-archive", false, "Allow statuses omitted from the file to be archived")
	projectLifecycleApplyCmd.Flags().Bool("dry-run", false, "Validate and show the apply plan without committing")
	projectLifecycleApplyCmd.Flags().String("output", "json", "Output format: yaml or json")
	projectLifecycleUseDefaultCmd.Flags().String("output", "json", "Output format: json")
	issueCmd.AddCommand(issueLifecycleStatusCmd)
	issueLifecycleStatusCmd.Flags().String("output", "table", "Output format: table or json")
}

func readLifecycleFile(cmd *cobra.Command, path, flagName string) (lifecycleFileSpec, error) {
	if strings.TrimSpace(path) == "" {
		return lifecycleFileSpec{}, fmt.Errorf("--%s is required", flagName)
	}
	if err := ensureFileFlagWithinWorkdir(cmd, flagName, "lifecycle", path); err != nil {
		return lifecycleFileSpec{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return lifecycleFileSpec{}, fmt.Errorf("read lifecycle file: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var spec lifecycleFileSpec
	if err := decoder.Decode(&spec); err != nil {
		return lifecycleFileSpec{}, fmt.Errorf("decode lifecycle file: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return lifecycleFileSpec{}, errors.New("lifecycle file must contain exactly one document")
		}
		return lifecycleFileSpec{}, fmt.Errorf("decode lifecycle file: %w", err)
	}
	return spec, nil
}

func resolveLifecyclePrincipal(ctx context.Context, client *cli.APIClient, principal lifecycleFilePrincipal, executor bool) (issuelifecycle.EntryPolicyPrincipal, error) {
	typeName := strings.ToLower(strings.TrimSpace(principal.Type))
	if typeName == "" {
		if executor {
			typeName = issuelifecycle.ExecutorNone
		} else {
			typeName = issuelifecycle.AssigneeKeep
		}
	}
	if typeName == issuelifecycle.AssigneeKeep || typeName == issuelifecycle.ExecutorNone {
		if strings.TrimSpace(principal.Ref) != "" {
			return issuelifecycle.EntryPolicyPrincipal{}, fmt.Errorf("%s does not accept ref", typeName)
		}
		return issuelifecycle.EntryPolicyPrincipal{Type: typeName}, nil
	}
	var kinds assigneeKinds
	switch typeName {
	case issuelifecycle.AssigneeHuman:
		if executor {
			return issuelifecycle.EntryPolicyPrincipal{}, errors.New("executor type cannot be human")
		}
		kinds = memberOnlyKinds
	case "agent":
		kinds = assigneeKinds{agent: true}
	case "squad":
		kinds = assigneeKinds{squad: true}
	default:
		return issuelifecycle.EntryPolicyPrincipal{}, fmt.Errorf("unsupported principal type %q", typeName)
	}
	if strings.TrimSpace(principal.Ref) == "" {
		return issuelifecycle.EntryPolicyPrincipal{}, fmt.Errorf("%s ref is required", typeName)
	}
	resolvedType, id, err := resolveAssignee(ctx, client, principal.Ref, kinds)
	if err != nil {
		return issuelifecycle.EntryPolicyPrincipal{}, err
	}
	if typeName == issuelifecycle.AssigneeHuman && resolvedType != "member" {
		return issuelifecycle.EntryPolicyPrincipal{}, errors.New("human ref did not resolve to a member")
	}
	return issuelifecycle.EntryPolicyPrincipal{Type: typeName, ID: id}, nil
}

func resolveLifecycleFileSpec(ctx context.Context, client *cli.APIClient, file lifecycleFileSpec) (lifecycleAPISpec, error) {
	result := lifecycleAPISpec{APIVersion: file.APIVersion, Name: file.Name, InitialStatus: file.InitialStatus, Statuses: make([]lifecycleAPIStatus, 0, len(file.Statuses))}
	principalCache := make(map[string]issuelifecycle.EntryPolicyPrincipal)
	resolve := func(principal lifecycleFilePrincipal, executor bool) (issuelifecycle.EntryPolicyPrincipal, error) {
		key := fmt.Sprintf("%t:%s:%s", executor, strings.ToLower(strings.TrimSpace(principal.Type)), strings.TrimSpace(principal.Ref))
		if cached, ok := principalCache[key]; ok {
			return cached, nil
		}
		resolved, err := resolveLifecyclePrincipal(ctx, client, principal, executor)
		if err == nil {
			principalCache[key] = resolved
		}
		return resolved, err
	}
	for i, status := range file.Statuses {
		assignee, err := resolve(status.EntryPolicy.Assignee, false)
		if err != nil {
			return lifecycleAPISpec{}, fmt.Errorf("statuses[%d].entry_policy.assignee: %w", i, err)
		}
		executor, err := resolve(status.EntryPolicy.Executor, true)
		if err != nil {
			return lifecycleAPISpec{}, fmt.Errorf("statuses[%d].entry_policy.executor: %w", i, err)
		}
		result.Statuses = append(result.Statuses, lifecycleAPIStatus{
			Key: status.Key, Name: status.Name, Description: status.Description,
			Color: status.Color, Phase: status.Phase,
			EntryPolicy: issuelifecycle.EntryPolicy{Assignee: assignee, Executor: executor, Instructions: status.EntryPolicy.Instructions, Advance: status.EntryPolicy.Advance},
		})
	}
	return result, nil
}

func lifecycleResponseToFile(response lifecycleAPIResponse, includeArchived bool) lifecycleFileSpec {
	file := lifecycleFileSpec{APIVersion: 1, Name: response.Lifecycle.Name, Statuses: make([]lifecycleFileStatus, 0, len(response.Statuses))}
	for _, status := range response.Statuses {
		if status.ArchivedAt != nil && !includeArchived {
			continue
		}
		if response.Lifecycle.InitialStatusID != nil && status.ID == *response.Lifecycle.InitialStatusID {
			file.InitialStatus = status.SpecKey
		}
		file.Statuses = append(file.Statuses, lifecycleFileStatus{
			Key: status.SpecKey, Name: status.Name, Description: status.Description, Color: status.Color, Phase: status.Phase,
			EntryPolicy: lifecycleFileEntryPolicy{
				Assignee:     lifecycleFilePrincipal{Type: status.EntryPolicy.Assignee.Type, Ref: status.EntryPolicy.Assignee.ID},
				Executor:     lifecycleFilePrincipal{Type: status.EntryPolicy.Executor.Type, Ref: status.EntryPolicy.Executor.ID},
				Instructions: status.EntryPolicy.Instructions, Advance: status.EntryPolicy.Advance,
			},
		})
	}
	return file
}

func resolveLifecycleStatusRef(ctx context.Context, client *cli.APIClient, projectID, ref string) (string, error) {
	params := url.Values{}
	if projectID != "" {
		params.Set("project_id", projectID)
	}
	path := "/api/issue-lifecycles/effective"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var response lifecycleAPIResponse
	if err := client.GetJSON(ctx, path, &response); err != nil {
		return "", err
	}
	input := strings.TrimSpace(ref)
	var matches []string
	for _, status := range response.Statuses {
		if status.ArchivedAt != nil {
			continue
		}
		if strings.EqualFold(status.ID, input) || strings.EqualFold(status.SpecKey, input) || strings.EqualFold(status.Name, input) {
			matches = append(matches, status.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("lifecycle status %q is ambiguous", ref)
	}
	return "", fmt.Errorf("no active lifecycle status matches %q", ref)
}

func resolveLifecycleStatusInDefinition(ctx context.Context, client *cli.APIClient, lifecycleID, ref string) (string, error) {
	var response lifecycleAPIResponse
	if err := client.GetJSON(ctx, "/api/issue-lifecycles/"+url.PathEscape(lifecycleID), &response); err != nil {
		return "", err
	}
	input := strings.TrimSpace(ref)
	for _, status := range response.Statuses {
		if status.ArchivedAt == nil && (strings.EqualFold(status.ID, input) || strings.EqualFold(status.SpecKey, input) || strings.EqualFold(status.Name, input)) {
			return status.ID, nil
		}
	}
	return "", fmt.Errorf("no active lifecycle status matches %q", ref)
}

func printLifecycleOutput(output string, value any) error {
	switch output {
	case "json":
		return cli.PrintJSON(os.Stdout, value)
	case "yaml":
		raw, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(raw)
		return err
	default:
		return fmt.Errorf("invalid output %q; valid values: yaml, json", output)
	}
}

func runProjectLifecycleGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	params := url.Values{"project_id": []string{project.ID}}
	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	if includeArchived {
		params.Set("include_archived", "true")
	}
	var response lifecycleAPIResponse
	if err := client.GetJSON(ctx, "/api/issue-lifecycles/effective?"+params.Encode(), &response); err != nil {
		return fmt.Errorf("get project lifecycle: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if includeArchived && output == "yaml" {
		return errors.New("--include-archived requires --output json because archived nodes are not part of an applyable lifecycle file")
	}
	if output == "json" {
		return printLifecycleOutput(output, response)
	}
	return printLifecycleOutput(output, lifecycleResponseToFile(response, includeArchived))
}

func runProjectLifecycleApply(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "json" && output != "yaml" {
		return fmt.Errorf("invalid output %q; valid values: yaml, json", output)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	path, _ := cmd.Flags().GetString("file")
	file, err := readLifecycleFile(cmd, path, "file")
	if err != nil {
		return err
	}
	spec, err := resolveLifecycleFileSpec(ctx, client, file)
	if err != nil {
		return fmt.Errorf("resolve lifecycle references: %w", err)
	}
	body := map[string]any{"mode": "custom", "spec": spec}
	if cmd.Flags().Changed("expected-revision") {
		revision, _ := cmd.Flags().GetInt64("expected-revision")
		if revision <= 0 {
			return errors.New("--expected-revision must be a positive integer")
		}
		body["expected_revision"] = revision
	}
	if allow, _ := cmd.Flags().GetBool("allow-archive"); allow {
		body["allow_archive"] = true
	}
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		body["dry_run"] = true
	}
	var response lifecycleAPIResponse
	if err := client.PutJSON(ctx, "/api/projects/"+project.ID+"/issue-lifecycle", body, &response); err != nil {
		return fmt.Errorf("apply project lifecycle: %w", err)
	}
	if output == "yaml" {
		return printLifecycleOutput(output, lifecycleResponseToFile(response, false))
	}
	return printLifecycleOutput(output, response)
}

func runProjectLifecycleUseDefault(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("invalid output %q; valid value: json", output)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var response lifecycleAPIResponse
	if err := client.PutJSON(ctx, "/api/projects/"+project.ID+"/issue-lifecycle", map[string]any{"mode": "default"}, &response); err != nil {
		return fmt.Errorf("use workspace lifecycle: %w", err)
	}
	return printLifecycleOutput(output, response)
}

func runIssueLifecycleStatus(cmd *cobra.Command, args []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "table" && output != "json" {
		return fmt.Errorf("invalid output %q; valid values: table, json", output)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	issue, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	var current map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issue.ID), &current); err != nil {
		return fmt.Errorf("get issue: %w", err)
	}
	lifecycleID := strVal(current, "lifecycle_id")
	if lifecycleID == "" {
		return errors.New("issue does not have a lifecycle binding")
	}
	statusID, err := resolveLifecycleStatusInDefinition(ctx, client, lifecycleID, args[1])
	if err != nil {
		return fmt.Errorf("resolve lifecycle status: %w", err)
	}
	var response map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issue.ID)+"/transitions", map[string]any{"lifecycle_status_id": statusID}, &response); err != nil {
		return fmt.Errorf("transition issue lifecycle status: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, response)
	}
	result, _ := response["issue"].(map[string]any)
	fmt.Fprintf(os.Stderr, "Issue %s transitioned to %s.\n", issueDisplayKey(result), args[1])
	return nil
}
