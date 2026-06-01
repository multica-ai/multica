package toolpolicy

// table_repo.go is the per-resource half of the admin table (FIR-2505 slice 2).
//
// table.go emits one capability-wide row per tool in the capability register.
// Repo access is different: the three repo capabilities (read / checkout / push)
// are NOT runtime-reported tools in cerebro_capability, and each one is scoped to
// a specific repo URL (the resource_pattern added in slice 1). So instead of one
// "repo.checkout" row the table shows, per repo, the three capabilities — the
// "repo as a collapsible group" the admin screen renders in slice 3.
//
// The repo universe is the same one the daemon builds its checkout allowlist from
// (workspace.repos ∪ each project's github_repo resource), so an admin can author
// policy on exactly the repos an agent can be asked to check out.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// Repo capability keys. These mirror the canonical keys in the grants package
// (grants.CapabilityRepo*) but are restated here so the tool-policy table — the
// system that owns repo access going forward — carries no dependency on the
// grant control plane it replaces.
const (
	capRepoRead     = "repo.read"
	capRepoCheckout = "repo.checkout"
	capRepoPush     = "repo.push"
)

// repoCategory is the synthetic category every per-repo row carries so the admin
// table can cluster a repo's three capabilities into one collapsible group. It is
// not a capability-register category — no capability-wide row ever uses it.
const repoCategory = "repo"

// repoSource labels per-repo rows in the table's Source column; they are derived
// from the workspace/project repo set, not reported by a runtime scan.
const repoSource = "repo"

// repoCapability pairs a repo capability key with the human label its row shows.
// The slice order is the order the three rows render inside a repo group.
type repoCapability struct {
	key   string
	title string
}

var repoCapabilities = []repoCapability{
	{capRepoRead, "Read code"},
	{capRepoCheckout, "Check out"},
	{capRepoPush, "Push changes"},
}

// repoPolicyKey identifies one (tool, repo) cell so stored settings can be bucketed
// back onto the synthetic rows.
type repoPolicyKey struct {
	toolKey         string
	resourcePattern string
}

// repoPolicyLayers holds the explicit per-layer settings authored for one (tool,
// repo) cell. group settings are accumulated separately and combined with
// CombineGroups, mirroring how table.go folds the capability-wide rows.
type repoPolicyLayers struct {
	layers map[Layer]Setting
	groups []Setting
}

// appendRepoRows discovers the workspace's repos and appends, for each repo, one
// row per repo capability carrying that (tool, repo) cell's explicit per-layer
// settings and resolved Effective verdict. groupIDs is the already-resolved group
// set for the query's context (see Table), reused so this path resolves the Group
// layer against the same groups as the capability-wide rows.
//
// Repo rows are emitted on every view, including a runtime-scoped one: repo
// capabilities are not runtime-reported, so the runtime filter in table.go would
// otherwise hide them — but repo access is authored at all five layers (runtime
// included), so the admin must see them there too.
func (s *Store) appendRepoRows(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, out []TableRow) ([]TableRow, error) {
	repos, err := s.discoverRepoURLs(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return out, nil
	}

	settings, err := s.loadRepoPolicySettings(ctx, in, groupIDs)
	if err != nil {
		return nil, err
	}

	for _, repoURL := range repos {
		for _, c := range repoCapabilities {
			row := TableRow{
				ToolKey:         c.key,
				ResourcePattern: repoURL,
				Title:           c.title,
				Category:        repoCategory,
				Source:          repoSource,
				Layers:          map[Layer]Setting{},
			}
			if cell, ok := settings[repoPolicyKey{c.key, repoURL}]; ok {
				for l, set := range cell.layers {
					row.Layers[l] = set
				}
				if len(cell.groups) > 0 {
					row.Layers[LayerGroup] = CombineGroups(cell.groups...)
				}
			}
			row.Effective = Resolve(Input{Settings: row.Layers, Base: in.Base})
			out = append(out, row)
		}
	}
	return out, nil
}

// discoverRepoURLs returns the deduplicated, sorted set of repo URLs known to the
// workspace: the workspace-level repos plus every project's github_repo resource.
// URLs are taken verbatim (only trimmed) so they match the daemon's allowlist,
// which compares checkout URLs by exact string.
func (s *Store) discoverRepoURLs(ctx context.Context, workspaceID pgtype.UUID) ([]string, error) {
	seen := map[string]struct{}{}
	add := func(raw string) {
		if u := strings.TrimSpace(raw); u != "" {
			seen[u] = struct{}{}
		}
	}

	// Workspace-level repos: a JSON array of {"url": "..."} (RepoData).
	var reposJSON []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT repos FROM workspace WHERE id = $1`, workspaceID).Scan(&reposJSON); err != nil {
		return nil, fmt.Errorf("toolpolicy: load workspace repos: %w", err)
	}
	if len(reposJSON) > 0 {
		var repos []struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(reposJSON, &repos); err == nil {
			for _, r := range repos {
				add(r.URL)
			}
		}
	}

	// Project-bound repos: github_repo resources carry {"url": "..."}.
	rows, err := s.pool.Query(ctx,
		`SELECT resource_ref FROM project_resource
		 WHERE workspace_id = $1 AND resource_type = 'github_repo'`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load project repos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref []byte
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan project repo: %w", err)
		}
		var payload struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(ref, &payload) == nil {
			add(payload.URL)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate project repos: %w", err)
	}

	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return out, nil
}

// loadRepoPolicySettings fetches every explicit per-layer setting authored for a
// repo capability (resource_pattern non-empty) in the query's context, bucketed
// by (tool, repo). It mirrors the subject predicates of the capability-wide query
// in table.go so an absent (Valid=false) subject id never matches and that layer
// stays Inherit.
func (s *Store) loadRepoPolicySettings(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID) (map[repoPolicyKey]*repoPolicyLayers, error) {
	keys := make([]string, len(repoCapabilities))
	for i, c := range repoCapabilities {
		keys[i] = c.key
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.tool_key, p.resource_pattern, p.layer, p.setting
		FROM cerebro_tool_policy p
		WHERE p.workspace_id = $1
		  AND p.resource_pattern <> ''
		  AND p.tool_key = ANY($6::text[])
		  AND (
		    (p.layer = 'workspace' AND p.subject_id = $1) OR
		    (p.layer = 'runtime'   AND p.subject_id = $2) OR
		    (p.layer = 'agent'     AND p.subject_id = $3) OR
		    (p.layer = 'user'      AND p.subject_id = $4) OR
		    (p.layer = 'group'     AND p.subject_id = ANY($5::uuid[]))
		  )
	`, in.WorkspaceID, in.RuntimeID, in.AgentID, in.UserID, groupIDs, keys)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load repo policy settings: %w", err)
	}
	defer rows.Close()

	out := map[repoPolicyKey]*repoPolicyLayers{}
	for rows.Next() {
		var toolKey, resourcePattern, layer, setting string
		if err := rows.Scan(&toolKey, &resourcePattern, &layer, &setting); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan repo policy setting: %w", err)
		}
		key := repoPolicyKey{toolKey, resourcePattern}
		cell, ok := out[key]
		if !ok {
			cell = &repoPolicyLayers{layers: map[Layer]Setting{}}
			out[key] = cell
		}
		l := Layer(layer)
		set := Setting(setting)
		if l == LayerGroup {
			cell.groups = append(cell.groups, set)
		} else {
			cell.layers[l] = set
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate repo policy settings: %w", err)
	}
	return out, nil
}
