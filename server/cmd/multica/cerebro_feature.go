// CEREBRO-PATCH(cerebro-feature-cli): FIR-3009 — list cerebro feature flags
// from the CLI. The flag catalog (keys, labels, descriptions, defaults,
// groups) is generated from packages/cerebro-feature-flags/registry.ts by
// `pnpm generate:feature-catalog` and embedded here, so the CLI always
// describes the same flags as the settings UI. Effective values mirror the
// frontend resolution in packages/cerebro-feature-flags/store.ts: a LOCKED
// workspace override wins, then a personal override, then an unlocked
// workspace override, then the registry default.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

//go:embed cerebro_feature_catalog.json
var cerebroFeatureCatalogJSON []byte

type cerebroFeatureGroup struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type cerebroFeatureDef struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Default     bool   `json:"default"`
}

type cerebroFeatureCatalog struct {
	Groups []cerebroFeatureGroup `json:"groups"`
	Flags  []cerebroFeatureDef   `json:"flags"`
}

// cerebroFeatureOverrides is the GET /api/workspaces/{id}/feature-flags
// response shape (see server/internal/cerebro/feature_flags/handler.go).
type cerebroFeatureOverrides struct {
	Overrides          map[string]bool `json:"overrides"`
	WorkspaceOverrides map[string]bool `json:"workspace_overrides"`
	Locked             map[string]bool `json:"locked"`
}

// cerebroFeatureState is one resolved flag as printed by `feature list`.
type cerebroFeatureState struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Group       string `json:"group"`
	Default     bool   `json:"default"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	Source      string `json:"source"`
	Locked      bool   `json:"locked"`
	Description string `json:"description"`
}

func loadCerebroFeatureCatalog() (cerebroFeatureCatalog, error) {
	var catalog cerebroFeatureCatalog
	if err := json.Unmarshal(cerebroFeatureCatalogJSON, &catalog); err != nil {
		return catalog, fmt.Errorf("parse embedded feature catalog: %w", err)
	}
	return catalog, nil
}

// resolveCerebroFeature mirrors resolveFlag in
// packages/cerebro-feature-flags/store.ts and flagEffectiveState in
// packages/cerebro-feature-flags/flag-status.ts.
func resolveCerebroFeature(def cerebroFeatureDef, ov cerebroFeatureOverrides) cerebroFeatureState {
	state := cerebroFeatureState{
		Key:         def.Key,
		Label:       def.Label,
		Group:       def.Group,
		Default:     def.Default,
		Description: def.Description,
	}
	ws, wsOk := ov.WorkspaceOverrides[def.Key]
	if ov.Locked[def.Key] && wsOk {
		state.Enabled = ws
		state.Source = "workspace (locked)"
		state.Locked = true
		if ws {
			state.State = "forced on"
		} else {
			state.State = "forced off"
		}
		return state
	}
	if personal, ok := ov.Overrides[def.Key]; ok {
		state.Enabled = personal
		state.Source = "personal"
	} else if wsOk {
		state.Enabled = ws
		state.Source = "workspace"
	} else {
		state.Enabled = def.Default
		state.Source = "default"
	}
	if state.Enabled {
		state.State = "on"
	} else {
		state.State = "off"
	}
	return state
}

// cerebroFeatureStates resolves every catalog flag in settings-UI order:
// groups in catalog order, flags within a group in registry order.
func cerebroFeatureStates(catalog cerebroFeatureCatalog, ov cerebroFeatureOverrides) []cerebroFeatureState {
	states := make([]cerebroFeatureState, 0, len(catalog.Flags))
	for _, group := range catalog.Groups {
		for _, def := range catalog.Flags {
			if def.Group == group.Key {
				states = append(states, resolveCerebroFeature(def, ov))
			}
		}
	}
	// Flags referencing an unknown group (should not happen — the generator
	// validates) still get listed rather than silently dropped.
	known := make(map[string]bool, len(catalog.Groups))
	for _, group := range catalog.Groups {
		known[group.Key] = true
	}
	for _, def := range catalog.Flags {
		if !known[def.Group] {
			states = append(states, resolveCerebroFeature(def, ov))
		}
	}
	return states
}

var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Cerebro feature flags (cerebro)",
}

var featureListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cerebro features with their effective state for you in this workspace",
	RunE:  runFeatureList,
}

var featureGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Show one cerebro feature incl. description and how its value resolves",
	Args:  exactArgs(1),
	RunE:  runFeatureGet,
}

func init() {
	featureCmd.AddCommand(featureListCmd, featureGetCmd)
	featureListCmd.Flags().String("group", "", "Only list flags in this group (e.g. permissions, agents, issues)")
	featureListCmd.Flags().String("state", "", "Only list flags in this effective state: on or off (forced flags match their value)")
	featureListCmd.Flags().String("output", "table", "Output format: table or json")
	featureGetCmd.Flags().String("output", "table", "Output format: table or json")
}

func fetchCerebroFeatureOverrides(cmd *cobra.Command) (cerebroFeatureOverrides, error) {
	var ov cerebroFeatureOverrides
	client, err := newAPIClient(cmd)
	if err != nil {
		return ov, err
	}
	path := "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/feature-flags"
	if err := client.GetJSON(context.Background(), path, &ov); err != nil {
		return ov, fmt.Errorf("load feature-flag overrides: %w", err)
	}
	return ov, nil
}

func runFeatureList(cmd *cobra.Command, args []string) error {
	catalog, err := loadCerebroFeatureCatalog()
	if err != nil {
		return err
	}
	ov, err := fetchCerebroFeatureOverrides(cmd)
	if err != nil {
		return err
	}

	groupFilter, _ := cmd.Flags().GetString("group")
	groupFilter = strings.TrimSpace(strings.ToLower(groupFilter))
	stateFilter, _ := cmd.Flags().GetString("state")
	stateFilter = strings.TrimSpace(strings.ToLower(stateFilter))
	if stateFilter != "" && stateFilter != "on" && stateFilter != "off" {
		return fmt.Errorf("invalid --state %q: must be on or off", stateFilter)
	}

	states := cerebroFeatureStates(catalog, ov)
	filtered := make([]cerebroFeatureState, 0, len(states))
	for _, s := range states {
		if groupFilter != "" && s.Group != groupFilter {
			continue
		}
		if stateFilter == "on" && !s.Enabled {
			continue
		}
		if stateFilter == "off" && s.Enabled {
			continue
		}
		filtered = append(filtered, s)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(filtered)
	}

	if groupFilter != "" && len(filtered) == 0 {
		keys := make([]string, 0, len(catalog.Groups))
		for _, g := range catalog.Groups {
			keys = append(keys, g.Key)
		}
		return fmt.Errorf("no flags in group %q: known groups are %s", groupFilter, strings.Join(keys, ", "))
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tSTATE\tSOURCE\tGROUP\tLABEL")
	for _, s := range filtered {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Key, s.State, s.Source, s.Group, s.Label)
	}
	return w.Flush()
}

func runFeatureGet(cmd *cobra.Command, args []string) error {
	catalog, err := loadCerebroFeatureCatalog()
	if err != nil {
		return err
	}
	key := strings.TrimSpace(args[0])
	var def *cerebroFeatureDef
	for i := range catalog.Flags {
		if catalog.Flags[i].Key == key {
			def = &catalog.Flags[i]
			break
		}
	}
	if def == nil {
		return fmt.Errorf("unknown feature flag %q: run 'multica feature list' to see all flags", key)
	}
	ov, err := fetchCerebroFeatureOverrides(cmd)
	if err != nil {
		return err
	}
	state := resolveCerebroFeature(*def, ov)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "Key:\t%s\n", state.Key)
	fmt.Fprintf(w, "Label:\t%s\n", state.Label)
	fmt.Fprintf(w, "Group:\t%s\n", state.Group)
	fmt.Fprintf(w, "State:\t%s\n", state.State)
	fmt.Fprintf(w, "Source:\t%s\n", state.Source)
	fmt.Fprintf(w, "Default:\t%t\n", state.Default)
	fmt.Fprintf(w, "Description:\t%s\n", state.Description)
	return w.Flush()
}
