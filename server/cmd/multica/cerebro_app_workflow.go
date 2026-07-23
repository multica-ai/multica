// CEREBRO-PATCH(cerebro-mini-app-workflow-cli): FIR-3497 route legacy app workflow users to the Workflows product.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var appWorkflowCmd = &cobra.Command{
	Use:    "workflow",
	Short:  "Legacy app workflow command",
	Hidden: true,
	RunE:   legacyAppWorkflowError,
}

var appWorkflowCreateCmd = &cobra.Command{
	Use:    "create",
	Short:  "Legacy app workflow create command",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   legacyAppWorkflowError,
}

func init() {
	// Keep accepting the removed command's flags so existing scripts receive
	// actionable migration guidance instead of an unrelated unknown-flag error.
	appWorkflowCreateCmd.Flags().String("app", "", "Legacy app ID")
	appWorkflowCreateCmd.Flags().String("name", "", "Legacy workflow name")
	appWorkflowCreateCmd.Flags().String("version", "1.0.0", "Legacy workflow schema version")
	appWorkflowCreateCmd.Flags().String("file", "", "Legacy workflow definition JSON file")
	appWorkflowCmd.AddCommand(appWorkflowCreateCmd)
}

func legacyAppWorkflowError(_ *cobra.Command, _ []string) error {
	return fmt.Errorf("the legacy app workflow contract was removed; rewrite the definition for the Workflows product and use multica workflow create --file <workflow.json>, which calls POST /api/cerebro/workflows")
}
