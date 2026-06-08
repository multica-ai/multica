package main

// TECH-3077: skill automation-link CLI extensions.
// Adds `multica skill impact <skill-id>` — shows automation impact analysis.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var skillImpactCmd = &cobra.Command{
	Use:   "impact <id>",
	Short: "Show which automations depend on a skill",
	Args:  exactArgs(1),
	RunE:  runSkillImpact,
}

func init() {
	skillCmd.AddCommand(skillImpactCmd)
}

func runSkillImpact(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	id := args[0]
	var impact struct {
		SkillID         string `json:"skill_id"`
		SkillName       string `json:"skill_name"`
		AutomationCount int    `json:"automation_count"`
		AutomationLinks []struct {
			Type     string `json:"type"`
			ID       string `json:"id"`
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
		} `json:"automation_links"`
	}
	if err := client.GetJSON(ctx, "/api/skills/"+id+"/impact", &impact); err != nil {
		return fmt.Errorf("skill impact: %w", err)
	}

	outputFmt, _ := cmd.Flags().GetString("output")
	if outputFmt == "json" {
		return cli.PrintJSON(os.Stdout, impact)
	}

	fmt.Printf("SKILL: %s\n\n", impact.SkillName)

	if len(impact.AutomationLinks) == 0 {
		fmt.Println("No automations linked to this skill.")
		fmt.Println("Add triggered_by: to the skill's YAML frontmatter to register links.")
		return nil
	}

	fmt.Printf("BRUGT AF %d AUTOMATION(S):\n", impact.AutomationCount)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("%-30s %-12s  %s\n", "NAME", "TYPE", "SCHEDULE")
	fmt.Println(strings.Repeat("-", 60))
	for _, l := range impact.AutomationLinks {
		sched := l.Schedule
		if sched == "" {
			sched = "-"
		}
		fmt.Printf("%-30s %-12s  %s\n", l.Name, l.Type, sched)
	}
	fmt.Printf("\nÆNDRER DU DENNE SKILL PÅVIRKES %d AUTOMATION(S).\n", impact.AutomationCount)
	return nil
}

// ensure json import is used
var _ = json.Marshal
