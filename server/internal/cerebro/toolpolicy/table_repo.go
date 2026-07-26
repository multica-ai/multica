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

	keys := make([]string, len(repoCapabilities))
	for i, c := range repoCapabilities {
		keys[i] = c.key
	}
	settings, err := s.loadResourcePolicySettings(ctx, in, groupIDs, resourcePolicyFilter{
		toolKeys: keys,
		scope:    resourcePatternNonEmpty,
	})
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
				Conditions:      map[Layer]*Condition{},
			}
			if cell, ok := settings[resourcePolicyKey{toolKey: c.key, resourcePattern: repoURL}]; ok {
				for l, set := range cell.layers {
					row.Layers[l] = set
				}
				for l, cond := range cell.conditions {
					row.Conditions[l] = cond
				}
				if len(cell.groups) > 0 {
					row.Layers[LayerGroup] = CombineGroups(cell.groups...)
				}
			}
			row.Effective, err = s.resolveTableResourcePermission(
				ctx, in, row.ToolKey, row.ResourcePattern, row.Layers, in.Base,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"toolpolicy: resolve repo permission %q on %q: %w",
					row.ToolKey, row.ResourcePattern, err,
				)
			}
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
