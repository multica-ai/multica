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
	"github.com/multica-ai/multica/server/internal/issueworkflow"
)

type workflowFilePrincipal struct {
	Type string `json:"type" yaml:"type"`
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

type workflowFileEntryPolicy struct {
	Assignee      workflowFilePrincipal `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	Executor      workflowFilePrincipal `json:"executor,omitempty" yaml:"executor,omitempty"`
	Instructions  string                `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	Advance       string                `json:"advance,omitempty" yaml:"advance,omitempty"`
	NextStatusKey string                `json:"next_status_key,omitempty" yaml:"next_status_key,omitempty"`
}

type workflowFileStatus struct {
	Key         string                  `json:"key" yaml:"key"`
	Name        string                  `json:"name" yaml:"name"`
	Description string                  `json:"description,omitempty" yaml:"description,omitempty"`
	Color       string                  `json:"color" yaml:"color"`
	Phase       string                  `json:"phase" yaml:"phase"`
	EntryPolicy workflowFileEntryPolicy `json:"entry_policy,omitempty" yaml:"entry_policy,omitempty"`
}

type workflowFileSpec struct {
	APIVersion    int                  `json:"api_version" yaml:"api_version"`
	Name          string               `json:"name" yaml:"name"`
	InitialStatus string               `json:"initial_status" yaml:"initial_status"`
	Statuses      []workflowFileStatus `json:"statuses" yaml:"statuses"`
}

type workflowAPISpec struct {
	APIVersion    int                 `json:"api_version"`
	Name          string              `json:"name"`
	InitialStatus string              `json:"initial_status"`
	Statuses      []workflowAPIStatus `json:"statuses"`
}

type workflowAPIStatus struct {
	Key         string                    `json:"key"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Color       string                    `json:"color"`
	Phase       string                    `json:"phase"`
	EntryPolicy issueworkflow.EntryPolicy `json:"entry_policy"`
}

type workflowAPIResponse struct {
	Workflow struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Revision        int64   `json:"revision"`
		InitialStatusID *string `json:"initial_status_id"`
	} `json:"workflow"`
	Statuses []struct {
		ID          string                    `json:"id"`
		SpecKey     string                    `json:"spec_key"`
		Name        string                    `json:"name"`
		Description string                    `json:"description"`
		Color       string                    `json:"color"`
		Phase       string                    `json:"phase"`
		ArchivedAt  *string                   `json:"archived_at"`
		EntryPolicy issueworkflow.EntryPolicy `json:"entry_policy"`
	} `json:"statuses"`
	Mode   string `json:"mode"`
	Plan   any    `json:"plan,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

var projectWorkflowCmd = &cobra.Command{Use: "workflow", Short: "Manage a project's issue workflow"}

var projectWorkflowGetCmd = &cobra.Command{
	Use: "get <project-id>", Short: "Export the effective workflow", Args: exactArgs(1), RunE: runProjectWorkflowGet,
}

var projectWorkflowApplyCmd = &cobra.Command{
	Use: "apply <project-id>", Short: "Apply a workflow YAML or JSON file", Args: exactArgs(1), RunE: runProjectWorkflowApply,
}

var projectWorkflowUseDefaultCmd = &cobra.Command{
	Use: "use-default <project-id>", Short: "Make a project inherit the workspace workflow", Args: exactArgs(1), RunE: runProjectWorkflowUseDefault,
}

var issueWorkflowStatusCmd = &cobra.Command{
	Use: "workflow-status <issue-id> <status>", Short: "Transition an issue by workflow status ID, stable key, or exact name", Args: exactArgs(2), RunE: runIssueWorkflowStatus,
}

func init() {
	projectCmd.AddCommand(projectWorkflowCmd)
	projectWorkflowCmd.AddCommand(projectWorkflowGetCmd, projectWorkflowApplyCmd, projectWorkflowUseDefaultCmd)
	projectWorkflowGetCmd.Flags().String("output", "yaml", "Output format: yaml or json")
	projectWorkflowGetCmd.Flags().Bool("include-archived", false, "Include archived status nodes")
	projectWorkflowApplyCmd.Flags().String("file", "", "Workflow YAML or JSON file (required)")
	projectWorkflowApplyCmd.Flags().Bool("allow-external-file", false, "Allow --file to read outside the current working directory")
	projectWorkflowApplyCmd.Flags().Int64("expected-revision", 0, "Only apply when the server workflow has this revision")
	projectWorkflowApplyCmd.Flags().Bool("allow-archive", false, "Allow statuses omitted from the file to be archived")
	projectWorkflowApplyCmd.Flags().Bool("dry-run", false, "Validate and show the apply plan without committing")
	projectWorkflowApplyCmd.Flags().String("output", "json", "Output format: yaml or json")
	projectWorkflowUseDefaultCmd.Flags().String("output", "json", "Output format: json")
	issueCmd.AddCommand(issueWorkflowStatusCmd)
	issueWorkflowStatusCmd.Flags().String("output", "table", "Output format: table or json")
}

func readWorkflowFile(cmd *cobra.Command, path, flagName string) (workflowFileSpec, error) {
	if strings.TrimSpace(path) == "" {
		return workflowFileSpec{}, fmt.Errorf("--%s is required", flagName)
	}
	if err := ensureFileFlagWithinWorkdir(cmd, flagName, "workflow", path); err != nil {
		return workflowFileSpec{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return workflowFileSpec{}, fmt.Errorf("read workflow file: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var spec workflowFileSpec
	if err := decoder.Decode(&spec); err != nil {
		return workflowFileSpec{}, fmt.Errorf("decode workflow file: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return workflowFileSpec{}, errors.New("workflow file must contain exactly one document")
		}
		return workflowFileSpec{}, fmt.Errorf("decode workflow file: %w", err)
	}
	return spec, nil
}

func resolveWorkflowPrincipal(ctx context.Context, client *cli.APIClient, principal workflowFilePrincipal, executor bool) (issueworkflow.EntryPolicyPrincipal, error) {
	typeName := strings.ToLower(strings.TrimSpace(principal.Type))
	if typeName == "" {
		if executor {
			typeName = issueworkflow.ExecutorNone
		} else {
			typeName = issueworkflow.AssigneeKeep
		}
	}
	if typeName == issueworkflow.AssigneeKeep || typeName == issueworkflow.ExecutorNone {
		if strings.TrimSpace(principal.Ref) != "" {
			return issueworkflow.EntryPolicyPrincipal{}, fmt.Errorf("%s does not accept ref", typeName)
		}
		return issueworkflow.EntryPolicyPrincipal{Type: typeName}, nil
	}
	var kinds assigneeKinds
	switch typeName {
	case issueworkflow.AssigneeHuman:
		if executor {
			return issueworkflow.EntryPolicyPrincipal{}, errors.New("executor type cannot be human")
		}
		kinds = memberOnlyKinds
	case "agent":
		kinds = assigneeKinds{agent: true}
	case "squad":
		kinds = assigneeKinds{squad: true}
	default:
		return issueworkflow.EntryPolicyPrincipal{}, fmt.Errorf("unsupported principal type %q", typeName)
	}
	if strings.TrimSpace(principal.Ref) == "" {
		return issueworkflow.EntryPolicyPrincipal{}, fmt.Errorf("%s ref is required", typeName)
	}
	resolvedType, id, err := resolveAssignee(ctx, client, principal.Ref, kinds)
	if err != nil {
		return issueworkflow.EntryPolicyPrincipal{}, err
	}
	if typeName == issueworkflow.AssigneeHuman && resolvedType != "member" {
		return issueworkflow.EntryPolicyPrincipal{}, errors.New("human ref did not resolve to a member")
	}
	return issueworkflow.EntryPolicyPrincipal{Type: typeName, ID: id}, nil
}

func resolveWorkflowFileSpec(ctx context.Context, client *cli.APIClient, file workflowFileSpec) (workflowAPISpec, error) {
	result := workflowAPISpec{APIVersion: file.APIVersion, Name: file.Name, InitialStatus: file.InitialStatus, Statuses: make([]workflowAPIStatus, 0, len(file.Statuses))}
	principalCache := make(map[string]issueworkflow.EntryPolicyPrincipal)
	resolve := func(principal workflowFilePrincipal, executor bool) (issueworkflow.EntryPolicyPrincipal, error) {
		key := fmt.Sprintf("%t:%s:%s", executor, strings.ToLower(strings.TrimSpace(principal.Type)), strings.TrimSpace(principal.Ref))
		if cached, ok := principalCache[key]; ok {
			return cached, nil
		}
		resolved, err := resolveWorkflowPrincipal(ctx, client, principal, executor)
		if err == nil {
			principalCache[key] = resolved
		}
		return resolved, err
	}
	for i, status := range file.Statuses {
		assignee, err := resolve(status.EntryPolicy.Assignee, false)
		if err != nil {
			return workflowAPISpec{}, fmt.Errorf("statuses[%d].entry_policy.assignee: %w", i, err)
		}
		executor, err := resolve(status.EntryPolicy.Executor, true)
		if err != nil {
			return workflowAPISpec{}, fmt.Errorf("statuses[%d].entry_policy.executor: %w", i, err)
		}
		result.Statuses = append(result.Statuses, workflowAPIStatus{
			Key: status.Key, Name: status.Name, Description: status.Description,
			Color: status.Color, Phase: status.Phase,
			EntryPolicy: issueworkflow.EntryPolicy{Assignee: assignee, Executor: executor, Instructions: status.EntryPolicy.Instructions, Advance: status.EntryPolicy.Advance, NextStatusKey: status.EntryPolicy.NextStatusKey},
		})
	}
	return result, nil
}

func workflowResponseToFile(response workflowAPIResponse, includeArchived bool) workflowFileSpec {
	file := workflowFileSpec{APIVersion: 1, Name: response.Workflow.Name, Statuses: make([]workflowFileStatus, 0, len(response.Statuses))}
	for _, status := range response.Statuses {
		if status.ArchivedAt != nil && !includeArchived {
			continue
		}
		if response.Workflow.InitialStatusID != nil && status.ID == *response.Workflow.InitialStatusID {
			file.InitialStatus = status.SpecKey
		}
		file.Statuses = append(file.Statuses, workflowFileStatus{
			Key: status.SpecKey, Name: status.Name, Description: status.Description, Color: status.Color, Phase: status.Phase,
			EntryPolicy: workflowFileEntryPolicy{
				Assignee:     workflowFilePrincipal{Type: status.EntryPolicy.Assignee.Type, Ref: status.EntryPolicy.Assignee.ID},
				Executor:     workflowFilePrincipal{Type: status.EntryPolicy.Executor.Type, Ref: status.EntryPolicy.Executor.ID},
				Instructions: status.EntryPolicy.Instructions, Advance: status.EntryPolicy.Advance, NextStatusKey: status.EntryPolicy.NextStatusKey,
			},
		})
	}
	return file
}

func resolveWorkflowStatusRef(ctx context.Context, client *cli.APIClient, projectID, ref string) (string, error) {
	params := url.Values{}
	if projectID != "" {
		params.Set("project_id", projectID)
	}
	path := "/api/issue-workflows/effective"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var response workflowAPIResponse
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
		return "", fmt.Errorf("workflow status %q is ambiguous", ref)
	}
	return "", fmt.Errorf("no active workflow status matches %q", ref)
}

func resolveWorkflowStatusInDefinition(ctx context.Context, client *cli.APIClient, workflowID, ref string) (string, error) {
	var response workflowAPIResponse
	if err := client.GetJSON(ctx, "/api/issue-workflows/"+url.PathEscape(workflowID), &response); err != nil {
		return "", err
	}
	input := strings.TrimSpace(ref)
	for _, status := range response.Statuses {
		if status.ArchivedAt == nil && (strings.EqualFold(status.ID, input) || strings.EqualFold(status.SpecKey, input) || strings.EqualFold(status.Name, input)) {
			return status.ID, nil
		}
	}
	return "", fmt.Errorf("no active workflow status matches %q", ref)
}

func printWorkflowOutput(output string, value any) error {
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

func runProjectWorkflowGet(cmd *cobra.Command, args []string) error {
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
	var response workflowAPIResponse
	if err := client.GetJSON(ctx, "/api/issue-workflows/effective?"+params.Encode(), &response); err != nil {
		return fmt.Errorf("get project workflow: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if includeArchived && output == "yaml" {
		return errors.New("--include-archived requires --output json because archived nodes are not part of an applyable workflow file")
	}
	if output == "json" {
		return printWorkflowOutput(output, response)
	}
	return printWorkflowOutput(output, workflowResponseToFile(response, includeArchived))
}

func runProjectWorkflowApply(cmd *cobra.Command, args []string) error {
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
	file, err := readWorkflowFile(cmd, path, "file")
	if err != nil {
		return err
	}
	spec, err := resolveWorkflowFileSpec(ctx, client, file)
	if err != nil {
		return fmt.Errorf("resolve workflow references: %w", err)
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
	var response workflowAPIResponse
	if err := client.PutJSON(ctx, "/api/projects/"+project.ID+"/issue-workflow", body, &response); err != nil {
		return fmt.Errorf("apply project workflow: %w", err)
	}
	if output == "yaml" {
		return printWorkflowOutput(output, workflowResponseToFile(response, false))
	}
	return printWorkflowOutput(output, response)
}

func runProjectWorkflowUseDefault(cmd *cobra.Command, args []string) error {
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
	var response workflowAPIResponse
	if err := client.PutJSON(ctx, "/api/projects/"+project.ID+"/issue-workflow", map[string]any{"mode": "default"}, &response); err != nil {
		return fmt.Errorf("use workspace workflow: %w", err)
	}
	return printWorkflowOutput(output, response)
}

func runIssueWorkflowStatus(cmd *cobra.Command, args []string) error {
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
	workflowID := strVal(current, "workflow_id")
	if workflowID == "" {
		return errors.New("issue does not have a workflow binding")
	}
	statusID, err := resolveWorkflowStatusInDefinition(ctx, client, workflowID, args[1])
	if err != nil {
		return fmt.Errorf("resolve workflow status: %w", err)
	}
	var response map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issue.ID)+"/transitions", map[string]any{"workflow_status_id": statusID}, &response); err != nil {
		return fmt.Errorf("transition issue workflow status: %w", err)
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, response)
	}
	result, _ := response["issue"].(map[string]any)
	fmt.Fprintf(os.Stderr, "Issue %s transitioned to %s.\n", issueDisplayKey(result), args[1])
	return nil
}
