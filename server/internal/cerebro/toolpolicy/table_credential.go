package toolpolicy

// table_credential.go is the per-credential half of the admin table (FIR-1479
// redesign). It is the AUTHORING SURFACE the canonical credential convention in
// cerebro_credentials_policy.go documents as missing — "until such an authoring
// surface exists the caps are inert." This file is that surface.
//
// table.go emits one capability-wide row per runtime-reported tool; table_repo.go
// adds the per-repo rows; this adds the per-credential rows. A credential (one
// Agent Vault box) is NOT a runtime-reported tool in cerebro_capability and each
// governance verb is scoped to a specific credential, so instead of one
// "credential.reveal" row the table shows, per credential box, the credential
// capabilities — grouped under the box's name exactly like a connection's tools.
//
// CRITICAL — the row ToolKey/ResourcePattern MUST be minted with the EXACT strings
// multicaCredentialPolicy.Check matches on, or an authored Allow/Deny silently
// misses the gate (resource_pattern is matched by equality, not glob):
//   ToolKey         = "credential.<attach|read_redacted|reveal|rotate|revoke>"
//   ResourcePattern = "cerebro-credential:<uuid>"   (id scope, most specific)
// This file therefore restates those keys locally (no import of the credentials
// package — that package imports toolpolicy, so the dependency must not reverse),
// the same way table_repo.go restates the repo capability keys.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// FlagCredentialsPerActor is the cerebro feature flag (registry.ts) that gates
// whether the per-credential authoring rows appear in the table. Default OFF: with
// no override the credential rows stay hidden, so prod behaviour is unchanged
// until an admin turns the flag on. This is the SAME user-facing flag the prior
// (parallel, retired) credentials column used — "credentials as a first-class
// permission type" — now backed by the unified tool-policy chain instead.
const FlagCredentialsPerActor = "cerebro_credentials_per_actor"

// Credential capability keys. These mirror the canonical permission verbs in the
// credentials package (credentials.Perm*) but are restated here because that
// package imports toolpolicy — the dependency must not reverse. They MUST match
// the strings multicaCredentialPolicy.Check mints ("credential.<verb>") or an
// authored row never reaches the gate.
const (
	capCredentialReveal       = "credential.reveal"
	capCredentialReadRedacted = "credential.read_redacted"
	capCredentialRotate       = "credential.rotate"
	capCredentialRevoke       = "credential.revoke"
	capCredentialAttach       = "credential.attach"
)

// credentialResourcePrefix is the id-scope ResourcePattern prefix the canonical
// convention pins (cerebro_credentials_policy.go). A row scoped to one credential
// carries credentialResourcePrefix + "<uuid>".
const credentialResourcePrefix = "cerebro-credential:"

// credentialCategory groups a credential box's capability rows in the admin table.
// Unlike repo (a single constant category + ResourcePattern), credentials follow
// the connection pattern: the box's own name is the Category, so the UI clusters
// that box's verbs under its name and the admin reads "give <box> to this actor".
// The constant below is only the Source label; the Category is per-box (the name).
const credentialSource = "credential"

// credentialCapability pairs a credential capability key with the human label its
// row shows. Slice order is the order the rows render inside a credential group.
type credentialCapability struct {
	key   string
	title string
}

var credentialCapabilities = []credentialCapability{
	{capCredentialReveal, "Use secret"},
	{capCredentialReadRedacted, "Read redacted"},
	{capCredentialRotate, "Rotate"},
	{capCredentialRevoke, "Revoke"},
	{capCredentialAttach, "Attach to resource"},
}

// vaultCredentialCapabilities is the capability set shown for an Agent Vault box
// (resource agentvault-vault:<name>). Only "Use secret" (credential.reveal)
// applies: the broker injects the box's secret read-only, and reveal is the ONLY
// verb the Agent Vault mirror projects to broker access
// (cerebro_agentvault_mirror.go, credentialBrokerAction).
// Rotate/revoke/attach/read-redacted belong to cerebro's
// own stored-secret model (cerebro-credential:<uuid>) and cannot act on a secret
// that lives in Agent Vault — showing them only confuses the admin.
var vaultCredentialCapabilities = []credentialCapability{
	{capCredentialReveal, "Use secret"},
}

// capabilitiesFor returns the capability rows to emit for a credential box: the
// single reveal verb for live Agent Vault boxes, the full set for cerebro-stored
// credentials.
func capabilitiesFor(resource string) []credentialCapability {
	if strings.HasPrefix(resource, agentvault.VaultResourcePrefix) {
		return vaultCredentialCapabilities
	}
	return credentialCapabilities
}

// credentialBox is one Agent Vault box (a row in cerebro_credential) the admin can
// author policy on: its id builds the ResourcePattern, its name labels the group.
type credentialBox struct {
	id   pgtype.UUID
	name string
}

// credentialResourceGroup is one grantable credential box as rendered in the
// table: its ResourcePattern (the exact grant target the policy gate matches on)
// and the Category the UI clusters the box's capability rows under (the box name).
// Two sources feed it — see discoverCredentialResources.
type credentialResourceGroup struct {
	resource string
	category string
}

// appendCredentialRows discovers the workspace's grantable credential boxes and
// appends, for each box, one row per credential capability carrying that
// (tool, credential) cell's explicit per-layer settings and resolved Effective
// verdict. groupIDs is the already-resolved group set for the query's context,
// reused so the Group layer resolves against the same groups as the other rows.
//
// Boxes come from two sources (discoverCredentialResources): credentials
// registered in cerebro_credential (cerebro-credential:<uuid>) and Agent Vault
// boxes (agentvault-vault:<name>). Most workspaces — including firtal — have an
// empty cerebro_credential table but real Agent Vault boxes, so without the
// second source the tab shows zero rows even though boxes exist (FIR-1739 last
// mile). The per-layer settings query loads rows for either resource shape, so a
// grant authored on either pattern surfaces here unchanged.
//
// Credential rows are emitted on every view, including a runtime-scoped one:
// credential capabilities are not runtime-reported, so the runtime filter in
// table.go would otherwise hide them — but credential access is authored at all
// actor layers (runtime included), exactly the reasoning appendRepoRows uses.
func (s *Store) appendCredentialRows(ctx context.Context, in TableQuery, groupIDs []pgtype.UUID, out []TableRow) ([]TableRow, error) {
	groups, err := s.discoverCredentialResources(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return out, nil
	}

	keys := make([]string, len(credentialCapabilities))
	for i, c := range credentialCapabilities {
		keys[i] = c.key
	}
	settings, err := s.loadResourcePolicySettings(ctx, in, groupIDs, resourcePolicyFilter{
		toolKeys: keys,
		scope:    resourcePatternNonEmpty,
	})
	if err != nil {
		return nil, err
	}

	for _, g := range groups {
		for _, c := range capabilitiesFor(g.resource) {
			row := TableRow{
				ToolKey:         c.key,
				ResourcePattern: g.resource,
				Title:           c.title,
				Category:        g.category,
				Source:          credentialSource,
				Layers:          map[Layer]Setting{},
				Conditions:      map[Layer]*Condition{},
			}
			if cell, ok := settings[resourcePolicyKey{toolKey: c.key, resourcePattern: g.resource}]; ok {
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
					"toolpolicy: resolve credential permission %q on %q: %w",
					row.ToolKey, row.ResourcePattern, err,
				)
			}
			out = append(out, row)
		}
	}
	return out, nil
}

// discoverCredentialResources returns every grantable credential box in the
// workspace as (resource, category) groups, from two sources:
//
//  1. Credentials registered in cerebro_credential — resource
//     cerebro-credential:<uuid>, the id-scoped pattern the canonical convention
//     pins. These carry a MULTICA_CREDENTIALS_KEY-encrypted secret.
//  2. Agent Vault boxes, when a vault lister is wired — resource
//     agentvault-vault:<name>, the vault-level pattern (FIR-1739 v1) the mirror
//     (agentvault/mirror.go) and canonical credential policy honor. Needs no
//     cerebro_credential row because the secret already lives in Agent Vault.
//
// A box registered in cerebro_credential AND present in Agent Vault is the SAME
// box (one box = one credential, keyed on name — migration 9096), so a vault
// whose name already appeared from source 1 is skipped: the id-scoped row wins
// and the box is never listed twice. Sorted by category for a stable table order.
func (s *Store) discoverCredentialResources(ctx context.Context, workspaceID pgtype.UUID) ([]credentialResourceGroup, error) {
	boxes, err := s.discoverCredentials(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	groups := make([]credentialResourceGroup, 0, len(boxes))
	seen := make(map[string]bool, len(boxes))
	for _, box := range boxes {
		groups = append(groups, credentialResourceGroup{
			resource: credentialResourcePrefix + util.UUIDToString(box.id),
			category: box.name,
		})
		seen[box.name] = true
	}
	for _, name := range s.agentVaultBoxNames(ctx, workspaceID) {
		if seen[name] {
			continue
		}
		groups = append(groups, credentialResourceGroup{
			resource: agentvault.VaultResourcePrefix + name,
			category: name,
		})
		seen[name] = true
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].category < groups[j].category })
	return groups, nil
}

// agentVaultBoxNames lists the Agent Vault box names for the credentials table,
// or nil when no vault lister is wired, or when the workspace has no "Agent
// Vault" connection — the same degradation as the vaults endpoint. A live
// ListVaults error is logged and downgraded to nil so an Agent Vault outage
// hides the vault-sourced rows but never fails the whole permissions table.
func (s *Store) agentVaultBoxNames(ctx context.Context, workspaceID pgtype.UUID) []string {
	if s.vaults == nil {
		return nil
	}
	vaults, err := s.vaults.ListVaults(ctx, workspaceID)
	if err != nil {
		slog.Error("toolpolicy: list agent vault boxes for credentials table failed", "error", err)
		return nil
	}
	names := make([]string, 0, len(vaults))
	for _, v := range vaults {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// discoverCredentials returns the workspace's registered credentials (rows in
// cerebro_credential) as (id, name) pairs, sorted by name. The wider grantable
// set (registered credentials plus Agent Vault boxes) is assembled by
// discoverCredentialResources.
func (s *Store) discoverCredentials(ctx context.Context, workspaceID pgtype.UUID) ([]credentialBox, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name FROM cerebro_credential
		 WHERE workspace_id = $1
		 ORDER BY name ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("toolpolicy: load workspace credentials: %w", err)
	}
	defer rows.Close()

	var out []credentialBox
	for rows.Next() {
		var box credentialBox
		if err := rows.Scan(&box.id, &box.name); err != nil {
			return nil, fmt.Errorf("toolpolicy: scan credential: %w", err)
		}
		out = append(out, box)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolpolicy: iterate credentials: %w", err)
	}
	return out, nil
}

// CredentialAuthoringEnabled reports whether the per-credential authoring rows
// should be appended to the table for this (workspace, user). The flag defaults
// OFF, so with no override anywhere it stays hidden; a DB error fails closed
// (show nothing new) and is logged. Resolution mirrors the canonical client
// precedence in packages/cerebro-feature-flags/store.ts (resolveFlag):
//
//  1. a LOCKED workspace override wins outright;
//  2. otherwise a personal override wins;
//  3. otherwise an unlocked workspace override (a soft workspace default);
//  4. otherwise the registry default (false for this flag).
func (s *Store) CredentialAuthoringEnabled(ctx context.Context, workspaceID, userID pgtype.UUID) bool {
	if s.q == nil {
		return false
	}
	// Workspace-level override lives under the all-zero sentinel user_id.
	var wsEnabled, wsLocked, wsFound bool
	wsRows, err := s.q.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		slog.Error("toolpolicy: workspace credentials-per-actor flag lookup failed", "error", err)
	} else {
		for _, r := range wsRows {
			if r.FlagKey == FlagCredentialsPerActor {
				wsEnabled, wsLocked, wsFound = r.Enabled, r.Locked, true
				break
			}
		}
	}
	// 1. A locked workspace override wins outright.
	if wsFound && wsLocked {
		return wsEnabled
	}
	// 2. Otherwise a personal override wins.
	if userID.Valid {
		on, perr := s.q.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
			WorkspaceID: workspaceID,
			UserID:      userID,
			FlagKey:     FlagCredentialsPerActor,
		})
		if perr == nil {
			return on
		}
		if !errors.Is(perr, pgx.ErrNoRows) {
			slog.Error("toolpolicy: credentials-per-actor flag lookup failed", "error", perr)
			return false
		}
	}
	// 3. Otherwise an unlocked workspace override (soft default).
	if wsFound {
		return wsEnabled
	}
	// 4. Otherwise the registry default — OFF for this flag.
	return false
}
