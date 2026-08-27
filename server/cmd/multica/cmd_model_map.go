package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var modelMapCmd = &cobra.Command{
	Use:   "model-map",
	Short: "Manage model tier map (global + per-workspace)",
}

var modelMapGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get model tier map",
	RunE:  runModelMapGet,
}

var modelMapSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set model tier map entries",
	RunE:  runModelMapSet,
}

var modelMapGetFallbackCmd = &cobra.Command{
	Use:   "get-fallback",
	Short: "Get model tier fallback chains",
	RunE:  runModelMapGetFallback,
}

var modelMapSetFallbackCmd = &cobra.Command{
	Use:   "set-fallback",
	Short: "Set model tier fallback chains",
	RunE:  runModelMapSetFallback,
}

func init() {
	modelMapCmd.AddCommand(modelMapGetCmd)
	modelMapCmd.AddCommand(modelMapSetCmd)
	modelMapCmd.AddCommand(modelMapGetFallbackCmd)
	modelMapCmd.AddCommand(modelMapSetFallbackCmd)

	modelMapGetCmd.Flags().Bool("global", false, "Use global map")
	modelMapGetCmd.Flags().String("workspace", "", "Workspace ID (defaults to current)")

	modelMapSetCmd.Flags().Bool("global", false, "Use global map")
	modelMapSetCmd.Flags().String("workspace", "", "Workspace ID (defaults to current)")
	modelMapSetCmd.Flags().String("cheap", "", "Concrete for tier cheap")
	modelMapSetCmd.Flags().String("balanced", "", "Concrete for tier balanced")
	modelMapSetCmd.Flags().String("premium", "", "Concrete for tier premium")

	modelMapGetFallbackCmd.Flags().Bool("global", false, "Use global map")
	modelMapGetFallbackCmd.Flags().String("workspace", "", "Workspace ID (defaults to current)")

	modelMapSetFallbackCmd.Flags().Bool("global", false, "Use global map")
	modelMapSetFallbackCmd.Flags().String("workspace", "", "Workspace ID (defaults to current)")
	modelMapSetFallbackCmd.Flags().String("cheap", "", "Comma-separated fallback chain for tier cheap")
	modelMapSetFallbackCmd.Flags().String("balanced", "", "Comma-separated fallback chain for tier balanced")
	modelMapSetFallbackCmd.Flags().String("premium", "", "Comma-separated fallback chain for tier premium")
}

func runModelMapGet(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}
	client := cli.NewAPIClient(serverURL, "", token)
	isGlobal, _ := cmd.Flags().GetBool("global")
	workspace, _ := cmd.Flags().GetString("workspace")

	var path string
	if isGlobal {
		path = "/api/model-map"
	} else {
		wsID := workspace
		if wsID == "" {
			wsID = resolveWorkspaceID(cmd)
		}
		if wsID == "" {
			return fmt.Errorf("--global or --workspace required")
		}
		path = "/api/workspaces/" + url.PathEscape(wsID) + "/model-map"
	}
	var out map[string]string
	if err := client.GetJSON(ctx, path, &out); err != nil {
		// fallback to settings endpoint if model-map not found
		if !isGlobal && workspace != "" {
			alt := "/api/workspaces/" + url.PathEscape(workspace) + "/settings"
			if err2 := client.GetJSON(ctx, alt, &out); err2 == nil {
				return cli.PrintJSON(os.Stdout, out)
			}
		}
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runModelMapSet(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}
	client := cli.NewAPIClient(serverURL, "", token)
	isGlobal, _ := cmd.Flags().GetBool("global")
	workspace, _ := cmd.Flags().GetString("workspace")

	body := map[string]string{}
	if v, _ := cmd.Flags().GetString("cheap"); v != "" {
		body["cheap"] = v
	}
	if v, _ := cmd.Flags().GetString("balanced"); v != "" {
		body["balanced"] = v
	}
	if v, _ := cmd.Flags().GetString("premium"); v != "" {
		body["premium"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("no tier values provided (--cheap/--balanced/--premium)")
	}

	var path string
	if isGlobal {
		path = "/api/model-map"
	} else {
		wsID := workspace
		if wsID == "" {
			wsID = resolveWorkspaceID(cmd)
		}
		if wsID == "" {
			return fmt.Errorf("--global or --workspace required")
		}
		path = "/api/workspaces/" + url.PathEscape(wsID) + "/model-map"
	}
	var out map[string]string
	if err := client.PatchJSON(ctx, path, body, &out); err != nil {
		// fallback to settings endpoint
		if !isGlobal {
			wsID := workspace
			if wsID == "" {
				wsID = resolveWorkspaceID(cmd)
			}
			alt := "/api/workspaces/" + url.PathEscape(wsID) + "/settings"
			if err2 := client.PatchJSON(ctx, alt, body, &out); err2 == nil {
				return cli.PrintJSON(os.Stdout, out)
			}
		}
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func splitCommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runModelMapGetFallback(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}
	client := cli.NewAPIClient(serverURL, "", token)
	isGlobal, _ := cmd.Flags().GetBool("global")
	workspace, _ := cmd.Flags().GetString("workspace")

	var path string
	if isGlobal {
		path = "/api/model-map/fallback"
	} else {
		wsID := workspace
		if wsID == "" {
			wsID = resolveWorkspaceID(cmd)
		}
		if wsID == "" {
			return fmt.Errorf("--global or --workspace required")
		}
		path = "/api/workspaces/" + url.PathEscape(wsID) + "/model-map/fallback"
	}
	var out map[string][]string
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runModelMapSetFallback(cmd *cobra.Command, _ []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}
	client := cli.NewAPIClient(serverURL, "", token)
	isGlobal, _ := cmd.Flags().GetBool("global")
	workspace, _ := cmd.Flags().GetString("workspace")

	body := map[string][]string{}
	if v, _ := cmd.Flags().GetString("cheap"); v != "" {
		body["cheap"] = splitCommaList(v)
	}
	if v, _ := cmd.Flags().GetString("balanced"); v != "" {
		body["balanced"] = splitCommaList(v)
	}
	if v, _ := cmd.Flags().GetString("premium"); v != "" {
		body["premium"] = splitCommaList(v)
	}
	if len(body) == 0 {
		return fmt.Errorf("no tier values provided (--cheap/--balanced/--premium)")
	}

	var path string
	if isGlobal {
		path = "/api/model-map/fallback"
	} else {
		wsID := workspace
		if wsID == "" {
			wsID = resolveWorkspaceID(cmd)
		}
		if wsID == "" {
			return fmt.Errorf("--global or --workspace required")
		}
		path = "/api/workspaces/" + url.PathEscape(wsID) + "/model-map/fallback"
	}
	var out map[string][]string
	if err := client.PatchJSON(ctx, path, body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}
