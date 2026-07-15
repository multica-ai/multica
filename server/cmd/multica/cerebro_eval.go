// CEREBRO-PATCH(cerebro-eval-cli): FIR-3308 versioned eval catalog and run tools.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var evalCmd = &cobra.Command{Use: "eval", Short: "Manage versioned eval contracts and workflow gates"}

func init() {
	commands := []*cobra.Command{
		{Use: "list", Short: "List evals", Args: cobra.NoArgs, RunE: runEvalList},
		{Use: "get <eval-id>", Short: "Get an eval", Args: exactArgs(1), RunE: runEvalGet},
		{Use: "create", Short: "Create an eval from JSON", Args: cobra.NoArgs, RunE: runEvalCreate},
		{Use: "update <eval-id>", Short: "Replace an eval from JSON", Args: exactArgs(1), RunE: runEvalUpdate},
		{Use: "delete <eval-id>", Short: "Delete an eval", Args: exactArgs(1), RunE: runEvalDelete},
		{Use: "runs <eval-id>", Short: "List eval runs", Args: exactArgs(1), RunE: runEvalRuns},
		{Use: "record-run <eval-id>", Short: "Record an immutable eval run from JSON", Args: exactArgs(1), RunE: runEvalRecordRun},
		{Use: "bindings", Short: "List workflow eval bindings", Args: cobra.NoArgs, RunE: runEvalBindings},
		{Use: "bind", Short: "Bind an eval to a workflow from JSON", Args: cobra.NoArgs, RunE: runEvalBind},
		{Use: "unbind <binding-id>", Short: "Delete a workflow eval binding", Args: exactArgs(1), RunE: runEvalUnbind},
	}
	for _, command := range commands {
		command.Flags().String("output", "json", "Output format: json")
	}
	for _, command := range []*cobra.Command{commands[2], commands[3], commands[6], commands[8]} {
		command.Flags().String("file", "", "Read the complete JSON document from a file")
		command.Flags().Bool("stdin", false, "Read the complete JSON document from stdin")
	}
	evalCmd.AddCommand(commands...)
	workflowCmd.AddCommand(evalCmd)
}

func evalClient(cmd *cobra.Command) (*cli.APIClient, context.Context, context.CancelFunc, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	return client, ctx, cancel, nil
}

func readEvalJSON(cmd *cobra.Command) (map[string]any, error) {
	file, _ := cmd.Flags().GetString("file")
	stdin, _ := cmd.Flags().GetBool("stdin")
	if (file == "") == !stdin {
		return nil, fmt.Errorf("exactly one of --file or --stdin is required")
	}
	var raw []byte
	var err error
	if file != "" {
		raw, err = os.ReadFile(file)
	} else {
		raw, err = io.ReadAll(cmd.InOrStdin())
	}
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil || body == nil {
		return nil, fmt.Errorf("JSON must be an object")
	}
	return body, nil
}

func evalGet(cmd *cobra.Command, path string) error {
	client, ctx, cancel, err := evalClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func evalWrite(cmd *cobra.Command, method, path string) error {
	body, err := readEvalJSON(cmd)
	if err != nil {
		return err
	}
	client, ctx, cancel, err := evalClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if method == "PUT" {
		err = client.PutJSON(ctx, path, body, &out)
	} else {
		err = client.PostJSON(ctx, path, body, &out)
	}
	if err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func evalDelete(cmd *cobra.Command, path, id string) error {
	client, ctx, cancel, err := evalClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	if err := client.DeleteJSON(ctx, path); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, map[string]any{"deleted": id})
}

func runEvalList(cmd *cobra.Command, _ []string) error { return evalGet(cmd, "/api/cerebro/evals") }
func runEvalGet(cmd *cobra.Command, a []string) error {
	return evalGet(cmd, "/api/cerebro/evals/"+url.PathEscape(a[0]))
}
func runEvalCreate(cmd *cobra.Command, _ []string) error {
	return evalWrite(cmd, "POST", "/api/cerebro/evals")
}
func runEvalUpdate(cmd *cobra.Command, a []string) error {
	return evalWrite(cmd, "PUT", "/api/cerebro/evals/"+url.PathEscape(a[0]))
}
func runEvalDelete(cmd *cobra.Command, a []string) error {
	return evalDelete(cmd, "/api/cerebro/evals/"+url.PathEscape(a[0]), a[0])
}
func runEvalRuns(cmd *cobra.Command, a []string) error {
	return evalGet(cmd, "/api/cerebro/evals/"+url.PathEscape(a[0])+"/runs")
}
func runEvalRecordRun(cmd *cobra.Command, a []string) error {
	return evalWrite(cmd, "POST", "/api/cerebro/evals/"+url.PathEscape(a[0])+"/runs")
}
func runEvalBindings(cmd *cobra.Command, _ []string) error {
	return evalGet(cmd, "/api/cerebro/evals/bindings")
}
func runEvalBind(cmd *cobra.Command, _ []string) error {
	return evalWrite(cmd, "POST", "/api/cerebro/evals/bindings")
}
func runEvalUnbind(cmd *cobra.Command, a []string) error {
	return evalDelete(cmd, "/api/cerebro/evals/bindings/"+url.PathEscape(a[0]), a[0])
}
