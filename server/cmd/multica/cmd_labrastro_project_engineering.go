package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func newLabrastroProjectEngineeringCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "engineering", Short: "Read or update project engineering stages"}
	for _, action := range []string{"get", "set"} {
		sub := &cobra.Command{Use: action + " <project-id>", Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return baselineFailure(2, "exactly one project ID is required")
			}
			return nil
		}, RunE: runLabrastroProjectEngineering}
		sub.Flags().String("output", "text", "Output format: text or json")
		if action == "set" {
			sub.Short = "Update engineering configuration as a human decider"
			sub.Flags().Int32("current-stage", 1, "Current stage (positive integer)")
			sub.Flags().String("mode-summary", "", "Engineering approach summary")
			sub.Flags().String("stages-file", "", "UTF-8 JSON stage array inside the current directory")
			sub.Flags().Bool("clear-stages", false, "Clear all configured stages")
		} else {
			sub.Short = "Read engineering configuration and stage goals"
		}
		cmd.AddCommand(sub)
	}
	return cmd
}

func runLabrastroProjectEngineering(cmd *cobra.Command, args []string) error {
	output := mustString(cmd, "output")
	if output != "text" && output != "table" && output != "json" {
		return baselineFailure(2, "unsupported --output format")
	}
	body := map[string]any{}
	if cmd.Name() == "set" {
		if cmd.Flags().Changed("stages-file") && cmd.Flags().Changed("clear-stages") {
			return baselineFailure(2, "--stages-file and --clear-stages are mutually exclusive")
		}
		if cmd.Flags().Changed("current-stage") {
			stage, _ := cmd.Flags().GetInt32("current-stage")
			if stage < 1 {
				return baselineFailure(2, "--current-stage must be positive")
			}
			body["current_stage"] = stage
		}
		if cmd.Flags().Changed("mode-summary") {
			body["mode_summary"] = mustString(cmd, "mode-summary")
		}
		if clear, _ := cmd.Flags().GetBool("clear-stages"); clear {
			body["stages"] = []any{}
		}
		if cmd.Flags().Changed("stages-file") {
			content, err := readProjectInputFile(mustString(cmd, "stages-file"), "stages-file", 1<<20)
			if err != nil {
				return err
			}
			var stages []json.RawMessage
			if json.Unmarshal([]byte(content), &stages) != nil || !strings.HasPrefix(strings.TrimSpace(content), "[") {
				return baselineFailure(2, "--stages-file must contain one JSON array")
			}
			body["stages"] = stages
		}
		if len(body) == 0 {
			return baselineFailure(2, "specify --current-stage, --mode-summary, --stages-file or --clear-stages")
		}
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return baselineFailure(1, "%s", err)
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var lookupErr error
	project, err := resolveIDByPrefix(ctx, client, "project", args[0], func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
		candidates, err := fetchProjectCandidates(ctx, client)
		lookupErr = err
		return candidates, err
	})
	if err != nil {
		if lookupErr != nil {
			return engineeringRequestError(lookupErr, false)
		}
		return baselineFailure(2, "%s", err)
	}
	path := "/api/projects/" + project.ID + "/engineering"
	var result struct {
		Engineering json.RawMessage `json:"engineering"`
	}
	if cmd.Name() == "set" {
		err = client.PatchJSON(ctx, path, body, &result)
	} else {
		err = client.GetJSON(ctx, path, &result)
	}
	if err != nil {
		return engineeringRequestError(err, cmd.Name() == "set")
	}
	var config struct {
		Enabled      bool   `json:"enabled"`
		CurrentStage int32  `json:"current_stage"`
		ModeSummary  string `json:"mode_summary"`
		Stages       []struct {
			Stage int64  `json:"stage"`
			Title string `json:"title"`
			Goal  string `json:"goal"`
		} `json:"stages"`
	}
	if json.Unmarshal(result.Engineering, &config) != nil || config.CurrentStage < 1 {
		return baselineFailure(1, "invalid engineering configuration response; inspect configuration before retrying")
	}
	if output == "json" {
		if err := cli.PrintJSON(cmd.OutOrStdout(), result.Engineering); err != nil {
			return baselineFailure(1, "cannot write engineering output")
		}
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Enabled: %t\nCurrent stage: %d\nMode summary: %s\n", config.Enabled, config.CurrentStage, config.ModeSummary)
	rows := make([][]string, 0, len(config.Stages))
	for _, stage := range config.Stages {
		rows = append(rows, []string{fmt.Sprint(stage.Stage), stage.Title, stage.Goal})
	}
	cli.PrintTable(cmd.OutOrStdout(), []string{"STAGE", "TITLE", "GOAL"}, rows)
	return nil
}

func engineeringRequestError(err error, writing bool) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 403 && writing {
			return baselineFailure(1, "工程配置只能由人类决定人修改，请在工程总览设置中修改")
		}
		if httpErr.StatusCode == 401 {
			return baselineRequestError(err, writing)
		}
		code := 1
		if httpErr.StatusCode == 400 || httpErr.StatusCode == 404 {
			code = 2
		}
		return baselineFailure(code, "engineering request failed (HTTP %d); inspect configuration before retrying", httpErr.StatusCode)
	}
	return baselineFailure(1, "engineering request failed; check project access and inspect configuration before retrying")
}
