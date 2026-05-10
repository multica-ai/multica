// CEREBRO-PATCH(autopilot-scope-cli): autopilot scope flags and request wiring (JEH-725).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	cerebroAutopilotScopeWorkspace = "workspace"
	cerebroAutopilotScopePersonal  = "personal"
	cerebroAutopilotScopeGroup     = "group"
)

var (
	cerebroAutopilotWriteScopes = map[string]bool{
		cerebroAutopilotScopeWorkspace: true,
		cerebroAutopilotScopePersonal:  true,
		cerebroAutopilotScopeGroup:     true,
	}
	cerebroAutopilotListScopes = map[string]bool{
		cerebroAutopilotScopeWorkspace: true,
		cerebroAutopilotScopePersonal:  true,
		cerebroAutopilotScopeGroup:     true,
		"mine":                         true,
		"shared":                       true,
		"all":                          true,
	}
)

type cerebroAutopilotScopeFlags struct {
	Scope        string
	Group        string
	Owner        string
	ScopeChanged bool
	GroupChanged bool
	OwnerChanged bool
}

type cerebroNamedID struct {
	ID   string
	Name string
}

func init() {
	cerebroAddAutopilotListScopeFlag(autopilotListCmd)
	cerebroAddAutopilotCreateScopeFlags(autopilotCreateCmd)
	cerebroAddAutopilotUpdateScopeFlags(autopilotUpdateCmd)
}

func cerebroAddAutopilotListScopeFlag(cmd *cobra.Command) {
	cmd.Flags().String("scope", "", "Filter by scope: workspace, personal, group, mine, shared, or all")
}

func cerebroAddAutopilotCreateScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("scope", cerebroAutopilotScopeWorkspace, "Autopilot scope: workspace, personal, or group")
	cmd.Flags().String("group", "", "Group name or ID (required with --scope group)")
	cmd.Flags().String("owner", "", "Owner member name or ID (personal scope; defaults to caller)")
}

func cerebroAddAutopilotUpdateScopeFlags(cmd *cobra.Command) {
	cmd.Flags().String("scope", "", "New autopilot scope: workspace, personal, or group")
	cmd.Flags().String("group", "", "New group name or ID")
	cmd.Flags().String("owner", "", "New owner member name or ID")
}

func cerebroAutopilotListScopePath(cmd *cobra.Command, path string) (string, error) {
	if !cmd.Flags().Changed("scope") {
		return path, nil
	}
	scope, _ := cmd.Flags().GetString("scope")
	scope = strings.TrimSpace(scope)
	if !cerebroAutopilotListScopes[scope] {
		return "", fmt.Errorf("--scope must be one of workspace, personal, group, mine, shared, or all")
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + url.Values{"scope": {scope}}.Encode(), nil
}

func cerebroAutopilotCreateScopeBody(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, body map[string]any) error {
	flags := cerebroReadAutopilotScopeFlags(cmd)
	if !flags.ScopeChanged && !flags.GroupChanged && !flags.OwnerChanged {
		return nil
	}
	if err := cerebroValidateAutopilotCreateScope(flags); err != nil {
		return err
	}

	body["scope"] = flags.Scope
	if flags.OwnerChanged {
		ownerID, err := cerebroResolveAutopilotOwner(ctx, client, flags.Owner)
		if err != nil {
			return fmt.Errorf("resolve owner: %w", err)
		}
		body["owner_user_id"] = ownerID
	}
	if flags.GroupChanged {
		groupID, err := cerebroResolveAutopilotGroup(ctx, client, flags.Group)
		if err != nil {
			return fmt.Errorf("resolve group: %w", err)
		}
		body["group_id"] = groupID
	}
	return nil
}

func cerebroAutopilotUpdateScopeBody(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, body map[string]any) error {
	flags := cerebroReadAutopilotScopeFlags(cmd)
	if !flags.ScopeChanged && !flags.GroupChanged && !flags.OwnerChanged {
		return nil
	}
	if err := cerebroValidateAutopilotUpdateScope(flags); err != nil {
		return err
	}

	if flags.ScopeChanged {
		body["scope"] = flags.Scope
	}
	if flags.OwnerChanged {
		ownerID, err := cerebroResolveAutopilotOwner(ctx, client, flags.Owner)
		if err != nil {
			return fmt.Errorf("resolve owner: %w", err)
		}
		body["owner_user_id"] = ownerID
	}
	if flags.GroupChanged {
		groupID, err := cerebroResolveAutopilotGroup(ctx, client, flags.Group)
		if err != nil {
			return fmt.Errorf("resolve group: %w", err)
		}
		body["group_id"] = groupID
	}
	return nil
}

func cerebroReadAutopilotScopeFlags(cmd *cobra.Command) cerebroAutopilotScopeFlags {
	scope, _ := cmd.Flags().GetString("scope")
	group, _ := cmd.Flags().GetString("group")
	owner, _ := cmd.Flags().GetString("owner")
	return cerebroAutopilotScopeFlags{
		Scope:        strings.TrimSpace(scope),
		Group:        strings.TrimSpace(group),
		Owner:        strings.TrimSpace(owner),
		ScopeChanged: cmd.Flags().Changed("scope"),
		GroupChanged: cmd.Flags().Changed("group"),
		OwnerChanged: cmd.Flags().Changed("owner"),
	}
}

func cerebroValidateAutopilotCreateScope(flags cerebroAutopilotScopeFlags) error {
	if !cerebroAutopilotWriteScopes[flags.Scope] {
		return fmt.Errorf("--scope must be one of workspace, personal, or group")
	}
	switch flags.Scope {
	case cerebroAutopilotScopeWorkspace:
		if flags.OwnerChanged {
			return fmt.Errorf("--owner requires --scope personal")
		}
		if flags.GroupChanged {
			return fmt.Errorf("--group requires --scope group")
		}
	case cerebroAutopilotScopePersonal:
		if flags.GroupChanged {
			return fmt.Errorf("--group cannot be used with --scope personal")
		}
	case cerebroAutopilotScopeGroup:
		if flags.OwnerChanged {
			return fmt.Errorf("--owner cannot be used with --scope group")
		}
		if !flags.GroupChanged || flags.Group == "" {
			return fmt.Errorf("--group is required when --scope group")
		}
	}
	return nil
}

func cerebroValidateAutopilotUpdateScope(flags cerebroAutopilotScopeFlags) error {
	if flags.ScopeChanged && !cerebroAutopilotWriteScopes[flags.Scope] {
		return fmt.Errorf("--scope must be one of workspace, personal, or group")
	}
	if !flags.ScopeChanged {
		if flags.OwnerChanged && flags.GroupChanged {
			return fmt.Errorf("--owner and --group cannot be updated together without --scope")
		}
		return nil
	}

	switch flags.Scope {
	case cerebroAutopilotScopeWorkspace:
		if flags.OwnerChanged {
			return fmt.Errorf("--owner requires --scope personal")
		}
		if flags.GroupChanged {
			return fmt.Errorf("--group requires --scope group")
		}
	case cerebroAutopilotScopePersonal:
		if flags.GroupChanged {
			return fmt.Errorf("--group cannot be used with --scope personal")
		}
	case cerebroAutopilotScopeGroup:
		if flags.OwnerChanged {
			return fmt.Errorf("--owner cannot be used with --scope group")
		}
		if !flags.GroupChanged || flags.Group == "" {
			return fmt.Errorf("--group is required when --scope group")
		}
	}
	return nil
}

func cerebroResolveAutopilotOwner(ctx context.Context, client *cli.APIClient, nameOrID string) (string, error) {
	input := strings.TrimSpace(nameOrID)
	if uuidRegexp.MatchString(input) {
		return input, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to resolve owners; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err != nil {
		return "", fmt.Errorf("fetch workspace members: %w", err)
	}
	candidates := make([]cerebroNamedID, 0, len(members))
	for _, m := range members {
		id := strVal(m, "user_id")
		if id == "" {
			id = strVal(m, "id")
		}
		name := strVal(m, "name")
		if name == "" {
			name = strVal(m, "email")
		}
		candidates = append(candidates, cerebroNamedID{ID: id, Name: name})
	}
	return cerebroResolveNamedID("owner", input, candidates)
}

func cerebroResolveAutopilotGroup(ctx context.Context, client *cli.APIClient, nameOrID string) (string, error) {
	input := strings.TrimSpace(nameOrID)
	if uuidRegexp.MatchString(input) {
		return input, nil
	}

	groups, err := cerebroFetchAutopilotGroups(ctx, client)
	if err != nil {
		return "", err
	}
	candidates := make([]cerebroNamedID, 0, len(groups))
	for _, g := range groups {
		candidates = append(candidates, cerebroNamedID{ID: strVal(g, "id"), Name: strVal(g, "name")})
	}
	return cerebroResolveNamedID("group", input, candidates)
}

func cerebroFetchAutopilotGroups(ctx context.Context, client *cli.APIClient) ([]map[string]any, error) {
	var raw json.RawMessage
	if err := client.GetJSON(ctx, "/api/cerebro/groups", &raw); err != nil {
		return nil, fmt.Errorf("fetch groups: %w", err)
	}

	var wrapped struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Groups != nil {
		return wrapped.Groups, nil
	}

	var groups []map[string]any
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("decode groups: %w", err)
	}
	return groups, nil
}

func cerebroResolveNamedID(kind, input string, candidates []cerebroNamedID) (string, error) {
	if input == "" {
		return "", fmt.Errorf("no %s found matching %q", kind, input)
	}
	inputLower := strings.ToLower(input)
	var idMatches, exactMatches, substringMatches []cerebroNamedID
	for _, candidate := range candidates {
		if candidate.ID == "" {
			continue
		}
		if strings.EqualFold(candidate.ID, input) || strings.EqualFold(truncateID(candidate.ID), input) {
			idMatches = append(idMatches, candidate)
			continue
		}
		if candidate.Name != "" && strings.EqualFold(candidate.Name, input) {
			exactMatches = append(exactMatches, candidate)
			continue
		}
		if candidate.Name != "" && strings.Contains(strings.ToLower(candidate.Name), inputLower) {
			substringMatches = append(substringMatches, candidate)
		}
	}

	for _, bucket := range [][]cerebroNamedID{idMatches, exactMatches, substringMatches} {
		switch len(bucket) {
		case 0:
			continue
		case 1:
			return bucket[0].ID, nil
		default:
			return "", cerebroAmbiguousNamedIDError(kind, input, bucket)
		}
	}
	return "", fmt.Errorf("no %s found matching %q", kind, input)
}

func cerebroAmbiguousNamedIDError(kind, input string, matches []cerebroNamedID) error {
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		parts = append(parts, fmt.Sprintf("  %q (%s)", match.Name, truncateID(match.ID)))
	}
	return fmt.Errorf("ambiguous %s %q; matches:\n%s", kind, input, strings.Join(parts, "\n"))
}
