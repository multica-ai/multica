package main

// CEREBRO-PATCH(skill-autolearn-cli): TECH-3692 — `multica skill auto-learn`
// turns a skill's self-learning switch on or off. New cerebro command.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var (
	autoLearnOn  bool
	autoLearnOff bool
)

var skillAutoLearnCmd = &cobra.Command{
	Use:   "auto-learn <id>",
	Short: "Turn a skill's self-learning (auto-learn) switch on or off",
	Long: `Turn a skill's self-learning switch on or off.

When auto-learn is on, the platform records the ways agents deviate from the
skill (behavioral observations) and the nightly learning review proposes
durable improvements for the owner to approve. When off, the skill stops
collecting observations.

Examples:
  multica skill auto-learn <id> --on
  multica skill auto-learn <id> --off`,
	Args: exactArgs(1),
	RunE: runSkillAutoLearn,
}

func init() {
	skillAutoLearnCmd.Flags().BoolVar(&autoLearnOn, "on", false, "Turn auto-learn on for the skill")
	skillAutoLearnCmd.Flags().BoolVar(&autoLearnOff, "off", false, "Turn auto-learn off for the skill")
	skillCmd.AddCommand(skillAutoLearnCmd)
}

func runSkillAutoLearn(cmd *cobra.Command, args []string) error {
	if autoLearnOn == autoLearnOff {
		return fmt.Errorf("specify exactly one of --on or --off")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result struct {
		SkillID   string `json:"skill_id"`
		SkillName string `json:"skill_name"`
		AutoLearn bool   `json:"auto_learn"`
	}
	body := map[string]bool{"enabled": autoLearnOn}
	if err := client.PostJSON(ctx, "/api/skills/"+args[0]+"/auto-learn", body, &result); err != nil {
		return fmt.Errorf("set auto-learn: %w", err)
	}

	outputFmt, _ := cmd.Flags().GetString("output")
	if outputFmt == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	state := "OFF"
	if result.AutoLearn {
		state = "ON"
	}
	fmt.Printf("Auto-learn for %q is now %s.\n", result.SkillName, state)
	return nil
}
