package main

// TECH-3077: skill self-learning CLI commands.
// `multica skill observations <id>` — show behavioral patterns for a skill.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var skillObservationsCmd = &cobra.Command{
	Use:   "observations <id>",
	Short: "Show behavioral patterns observed for a skill (self-learning data)",
	Args:  exactArgs(1),
	RunE:  runSkillObservations,
}

func init() {
	skillCmd.AddCommand(skillObservationsCmd)
}

func runSkillObservations(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	id := args[0]
	var result struct {
		SkillID   string `json:"skill_id"`
		SkillName string `json:"skill_name"`
		Patterns  []struct {
			DeltaType      string  `json:"delta_type"`
			DeltaValue     string  `json:"delta_value"`
			RunCount       int     `json:"run_count"`
			ConsistencyPct float64 `json:"consistency_pct"`
			Proposed       bool    `json:"proposed"`
			AboveThreshold bool    `json:"above_threshold"`
		} `json:"patterns"`
		Recent []struct {
			DeltaType  string  `json:"delta_type"`
			DeltaValue string  `json:"delta_value"`
			Outcome    string  `json:"outcome"`
			CreatedAt  string  `json:"created_at"`
		} `json:"recent"`
	}
	if err := client.GetJSON(ctx, "/api/skills/"+id+"/observations", &result); err != nil {
		return fmt.Errorf("skill observations: %w", err)
	}

	outputFmt, _ := cmd.Flags().GetString("output")
	if outputFmt == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("OBSERVATIONER FOR SKILL: %s\n\n", result.SkillName)

	if len(result.Patterns) == 0 {
		fmt.Println("Ingen adfærdsmønstre observeret endnu.")
		fmt.Println("Brug POST /api/skill-observations fra agent-runtimen for at registrere delta-adfærd.")
		return nil
	}

	fmt.Println("ADFÆRDSMØNSTRE:")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-22s  %-30s  %5s  %8s  %s\n", "TYPE", "DELTA", "RUNS", "KONSISTENS", "STATUS")
	fmt.Println(strings.Repeat("-", 80))
	for _, p := range result.Patterns {
		status := ""
		if p.Proposed {
			status = "FORSLAG SENDT"
		} else if p.AboveThreshold {
			status = "KLAR TIL FORSLAG"
		} else {
			status = fmt.Sprintf("(%d/%d runs)", p.RunCount, 10)
		}
		fmt.Printf("%-22s  %-30s  %5d  %7.0f%%  %s\n",
			p.DeltaType,
			truncate(p.DeltaValue, 30),
			p.RunCount,
			p.ConsistencyPct*100,
			status,
		)
	}

	if len(result.Recent) > 0 {
		fmt.Printf("\nSENESTE %d RUNS:\n", len(result.Recent))
		for _, r := range result.Recent {
			fmt.Printf("  %s  %-20s  %-30s  %s\n", r.CreatedAt[:19], r.DeltaType, truncate(r.DeltaValue, 30), r.Outcome)
		}
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
