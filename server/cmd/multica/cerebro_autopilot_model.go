// CEREBRO-PATCH(autopilot-model-cli): --model flag wiring for autopilot create/update (JEH-1310).
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cerebro/autopilotmodel"
)

const cerebroAutopilotModelClearSentinel = "none"

func init() {
	cerebroAddAutopilotModelFlag(autopilotCreateCmd, "")
	cerebroAddAutopilotModelFlag(autopilotUpdateCmd, cerebroAutopilotModelClearSentinel)
}

func cerebroAddAutopilotModelFlag(cmd *cobra.Command, clearHint string) {
	// Name a model per provider rather than listing the full catalog: it spans
	// every provider (~27 IDs) and only the handful its own runtime accepts will
	// work, so an exhaustive list is longer AND more misleading than an example.
	// The server validates against the assignee agent's runtime and returns the
	// accepted list for that provider on a bad value.
	help := fmt.Sprintf("Override the assignee agent's default model for every run of this autopilot. Must be a model the agent's runtime provider accepts (Claude runtime: %s; Codex runtime: gpt-5.5). Empty = agent default", autopilotmodel.ModelHaiku)
	if clearHint != "" {
		help += fmt.Sprintf(" (use --model %s to clear an existing override)", clearHint)
	}
	cmd.Flags().String("model", "", help)
}

// cerebroAutopilotCreateModelBody appends the optional model field to a
// CreateAutopilot request body. Empty flag → omit the field (the server
// stores NULL, so the autopilot inherits the agent default).
func cerebroAutopilotCreateModelBody(cmd *cobra.Command, body map[string]any) error {
	if !cmd.Flags().Changed("model") {
		return nil
	}
	model, _ := cmd.Flags().GetString("model")
	model = strings.TrimSpace(model)
	if err := autopilotmodel.Validate(model); err != nil {
		return err
	}
	body["model"] = model
	return nil
}

// cerebroAutopilotUpdateModelBody handles the PATCH form. `--model ""` and
// `--model none` both clear the override (server normalises empty → NULL).
// Explicit clear-sentinel is offered so shell history shows intent.
func cerebroAutopilotUpdateModelBody(cmd *cobra.Command, body map[string]any) error {
	if !cmd.Flags().Changed("model") {
		return nil
	}
	model, _ := cmd.Flags().GetString("model")
	model = strings.TrimSpace(model)
	if model == cerebroAutopilotModelClearSentinel {
		model = ""
	}
	if err := autopilotmodel.Validate(model); err != nil {
		return err
	}
	body["model"] = model
	return nil
}
