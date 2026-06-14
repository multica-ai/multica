package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ── squad route ───────────────────────────────────────────────────────────────

var squadRouteCmd = &cobra.Command{
	Use:   "route <query>",
	Short: "Find the right squad for a task",
	Long: `Match a task description against all squads' declared capabilities
using keyword overlap scoring. Returns ranked matches and lists
squads that have not declared capabilities yet.

Example:
  multica squad route "帮我分析一下转行 AI Infra 应该选哪个方向"`,
	Args: exactArgs(1),
	RunE: runSquadRoute,
}

func runSquadRoute(cmd *cobra.Command, args []string) error {
	query := args[0]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"query": query}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/squads/route", body, &result); err != nil {
		return fmt.Errorf("route: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	// Print human-readable output.
	matches, _ := result["matches"].([]any)
	if matches != nil && len(matches) > 0 {
		fmt.Println("匹配结果:")
		for i, m := range matches {
			match, _ := m.(map[string]any)
			score := intScore(match, "score")
			matched := strSlice(match, "matched_words")
			fmt.Printf("  #%d  %-25s 评分 %d  %s\n",
				i+1, strVal(match, "name"), score, strings.Join(matched, ", "))
		}
		fmt.Println()

		if rec, _ := result["recommend"].(map[string]any); rec != nil {
			fmt.Printf("推荐: %s\n\n", strVal(rec, "name"))
		}
	} else {
		fmt.Println("没有匹配的 squad。")
	}

	undeclared, _ := result["undeclared"].([]any)
	if undeclared != nil && len(undeclared) > 0 {
		fmt.Println("以下 squad 尚未声明能力:")
		for _, u := range undeclared {
			fmt.Printf("  - %v\n", u)
		}
	}

	return nil
}

func intScore(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func strSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func init() {
	squadRouteCmd.Flags().String("output", "table", "Output format: table or json")
}
