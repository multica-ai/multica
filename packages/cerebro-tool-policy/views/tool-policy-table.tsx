"use client";

// The FIR-2284 capability catalog — the redesigned permission flade approved by
// Jesper (UX matched 1:1 to the Claude-design handoff, skinned in Multica
// tokens). It replaces the old "pick a class first" navigation with ONE flat,
// sortable catalog of every tool the subject can use, narrowed by combinable
// filters: capability class, side effect, decision, and free-text search. Each
// row shows what the tool can do (class + side effect), the resolved decision as
// a single editable pill, and which layer of the Runtime › Agent › Group › User
// chain decided it ("Resolved by"). The SAME component renders on the agent page
// and the runtime page; `view` decides which layer this page authors.
//
// Bite A (this component) delivers the unified flade + filters + per-row decision
// editing + the resolved-by column + a mobile card layout. The argument-scope
// sub-rules from the mockup (Bash › git push:* etc.) need a new persisted model +
// enforcement and land in Bite B — this component is structured to grow the
// expandable scope rows without a rewrite.
//
// Everything resolves server-side: the row already carries its Effective verdict
// and the deciding layer, so the UI never resolves the chain itself. Schemas in
// ../core fail CLOSED (unknown verdict → deny), so a drifted response can never
// render a tool as Allow by accident.

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  ChevronDown,
  ChevronRight,
  Database,
  Folder,
  FolderGit2,
  Globe,
  Info,
  KeyRound,
  Loader2,
  Lock,
  Plus,
  Search,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
  SlidersHorizontal,
  UsersRound,
  X,
} from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  conditionFacets,
  isLockedFromElsewhere,
  permissionDescription,
  toolPolicyTableOptions,
  useClearToolPolicy,
  useSetToolPolicy,
  type ToolCondition,
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolPolicyRow,
  type ToolSetting,
} from "../core";
import {
  DATA_SOURCE_ARG,
  FOLDER_ARG,
  groupByFolder,
  useDataSourceScopeConfig,
  useScopeOptions,
  type ScopeConfig,
  type ScopeOption,
} from "./data-source-scope";
import { GROUP_ARG, useGroupScopeOptions } from "./group-scope";
import { FirtalRegistryRowConfigure } from "./firtal-registry-row-configure";
import { ConnectionRowConfigure } from "./connection-row-configure";
import { ConnectionToolList } from "./connection-config-sheet";
import {
  FirtalRegistryDataSourceConfigure,
  DataSourceList,
} from "./firtal-registry-data-source-sheet";
import { CapabilityCatalog } from "./capability-catalog";

/**
 * The actor surfaces. Each page renders the same catalog but authors a different
 * rung of the Workspace › Runtime › Agent › Group › User chain, OR the System
 * mandate of one autopilot. `view` decides the editable layer; `subjectId` is the
 * id that layer's rows key on for this page.
 *
 * `system` is an ACTOR on par with agents and members (FIR-1692): the autopilot's
 * own permissions page authors the System layer for that autopilot's human-less
 * runs. It is NOT a stacked layer hanging on the agent/member pages — those pages
 * only author their own rung.
 */
export type ToolPolicyView =
  | "workspace"
  | "runtime"
  | "agent"
  | "group"
  | "member"
  | "system";

// FIR-2281 split the permission flade into two tabs; FIR-2706 splits the
// former "resources" tab in two again, so the three resource kinds each get
// their own dedicated surface instead of being lumped together and told
// apart only by the Type column/filter:
//   "permissions" — every flat capability the subject can use (Multica + the
//                   runtime-reported tools). The simple on/off rights.
//   "repos"       — the repositories an agent is scoped to, one collapsible
//                   group per repo URL.
//   "connections" — workspace connections (flat rows) plus credential boxes
//                   (collapsible groups), since both are "access to an
//                   external resource" the same way a connection is.
export type ToolPolicyTabFilter = "permissions" | "repos" | "connections";

export interface ToolPolicyTableProps {
  wsId: string;
  /** Which page this table is on — decides the editable layer + write subject. */
  view: ToolPolicyView;
  /**
   * The id the page's layer keys on: the workspace id (workspace), runtime id
   * (runtime), agent id (agent), group id (group), or member's USER id (member).
   */
  subjectId: string;
  /** The runtime the agent runs on — shows the inherited Runtime column (agent view). */
  runtimeId?: string | null;
  /** The viewing user + their groups, so the Effective column reflects the ceiling. */
  userId?: string | null;
  groupIds?: string[];
  /**
   * Which slice of the flade this instance renders (FIR-2281, split further by FIR-2706):
   *   "permissions" — flat capability rows that are not a connection (Multica +
   *                   runtime-reported tools).
   *   "repos"       — the repo collapsible groups only.
   *   "connections" — connection rows (flat) plus the credential collapsible
   *                   groups.
   * When omitted, all rows are shown (the single-table fallback).
   */
  tabFilter?: ToolPolicyTabFilter;
}

export type ToolPolicyTabsProps = Omit<ToolPolicyTableProps, "tabFilter">;

// VIEW_EDIT_LAYER maps each surface to the chain layer it authors. The member
// page edits the User (ceiling) layer — "member" is the page, "user" is the rung.
// The system page edits the System layer — "system" is the page (an autopilot's
// own permissions page), "system" is the rung it authors.
const VIEW_EDIT_LAYER: Record<ToolPolicyView, ToolLayer> = {
  workspace: "workspace",
  runtime: "runtime",
  agent: "agent",
  group: "group",
  member: "user",
  system: "system",
};

// Repo rows carry a non-empty resource_pattern (the repo URL) and render as
// collapsible groups, not as flat rows in the tool catalog (FIR-2505 slice 3).
// The order the three repo capabilities render inside a group, regardless of the
// order the server emitted them.
const REPO_CAP_ORDER = ["repo.read", "repo.checkout", "repo.push"];

// Credential rows (FIR-1739) also carry a non-empty resource_pattern (the Agent
// Vault box, `agentvault-vault:<name>`, or `cerebro-credential:<uuid>`) and render
// as collapsible groups — one group per credential box — exactly like repos, so a
// box is ONE permission row you fold open to set each action. The order the box's
// capability rows render inside a group, regardless of server emission order.
const CREDENTIAL_CAP_ORDER = [
  "credential.reveal",
  "credential.read_redacted",
  "credential.rotate",
  "credential.revoke",
  "credential.attach",
];

const SETTING_CHOICES: ToolSetting[] = ["allow", "ask", "deny", "inherit"];
// "disable" (FIR-2351 follow-up, product decision 2026-07-06) only ever makes
// sense authored at the workspace layer — it turns a workspace Deny into a
// hard, unopenable floor for one permission, the opposite of what Group/User/
// Agent rows do. Only the workspace-layer decision control offers it; every
// other layer keeps SETTING_CHOICES unchanged. See settingChoicesFor below.
const WORKSPACE_SETTING_CHOICES: ToolSetting[] = ["allow", "ask", "deny", "disable", "inherit"];
function settingChoicesFor(editLayer: ToolLayer): ToolSetting[] {
  return editLayer === "workspace" ? WORKSPACE_SETTING_CHOICES : SETTING_CHOICES;
}
const SETTING_LABEL: Record<ToolSetting, string> = {
  allow: "Allow",
  ask: "Ask",
  deny: "Deny",
  inherit: "Inherit",
  disable: "Disable",
};
const DECISION_FILTERS: ToolEffectiveSetting[] = ["allow", "ask", "deny"];

// Restrictiveness rank — a sub-row choice can only TIGHTEN a group-wide floor,
// never loosen it (TECH-3287 hul 7). Mirrors the connection sheet's rank so the
// inline catalog lists and the sheet gate futile choices identically.
const SETTING_RANK: Record<ToolEffectiveSetting, number> = { allow: 0, ask: 1, deny: 2 };

// Decision palette — emerald / amber / destructive, matching the cerebro fork's
// other permission surfaces (simple table, cerebro-access).
const VERDICT_PILL: Record<ToolEffectiveSetting, string> = {
  allow:
    "border-emerald-600/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  ask: "border-amber-600/40 bg-amber-500/10 text-amber-700 dark:text-amber-400",
  deny: "border-destructive/30 bg-destructive/10 text-destructive",
};
const VERDICT_ICON: Record<ToolEffectiveSetting, typeof ShieldCheck> = {
  allow: ShieldCheck,
  ask: ShieldQuestion,
  deny: ShieldAlert,
};
// "Resolved by" layer → human label. "" means the system base default decided.
const LAYER_LABEL: Record<string, string> = {
  workspace: "Workspace",
  runtime: "Runtime",
  agent: "Agent",
  group: "Group",
  user: "User",
  system: "System",
  "": "Default",
};

// FIR-2281: the permission "Type" — the single dimension that used to be the
// five top-level tabs, now a column + filter. FIR-2706 maps it onto three
// tabs instead of two: "permissions" covers Multica + Runtime, "repos" covers
// Repos, and "connections" covers Connections + Credentials. ONE classifier
// drives the Type column AND the tab/filter partition, so a row can never
// land under the wrong type.
export type PermissionType =
  | "Multica"
  | "Runtime"
  | "Connections"
  | "Repos"
  | "Credentials";

const PERMISSION_TYPES_BY_TAB: Record<ToolPolicyTabFilter, PermissionType[]> = {
  permissions: ["Multica", "Runtime"],
  repos: ["Repos"],
  connections: ["Connections", "Credentials"],
};

// A runtime-reported tool is one a runtime actually advertised — built-ins and
// MCP actions land with source "runtime_report" (the snapshot) or "scan" (the
// daemon tools/list probe). The platform "Runtimes" category is the runtime
// admin actions (manage_runtime, create_runtime) from platformcatalog; both
// belong to the Runtime type.
function isRuntimeReportedSource(r: ToolPolicyRow): boolean {
  return r.source === "runtime_report" || r.source === "scan";
}

export function permissionType(r: ToolPolicyRow): PermissionType {
  if (r.source === "connection") return "Connections";
  if (r.source === "credential") return "Credentials";
  // Any other per-resource row (a non-empty pattern that is not a connection
  // sub-row or registry data source — those never reach the rendered sets) is a
  // repository group.
  if (r.resource_pattern) return "Repos";
  if (isRuntimeReportedSource(r) || r.category === "Runtimes") return "Runtime";
  return "Multica";
}

function TypeTag({ row }: { row: ToolPolicyRow }) {
  return (
    <Badge variant="outline" className="font-normal">
      {permissionType(row)}
    </Badge>
  );
}

export function ToolPolicyTable({
  wsId,
  view,
  subjectId,
  runtimeId,
  userId,
  groupIds,
  tabFilter,
}: ToolPolicyTableProps) {
  // Each surface assembles only the chain context it authors. The workspace
  // root layer is always loaded server-side from wsId, so the workspace view
  // passes no extra subject and still resolves its own layer's Effective column.
  // The System view is the autopilot's own permissions page: it authors the
  // System layer keyed on that autopilot (ctx.autopilotId → system_id), so the
  // backend resolves the chain with the autopilot's mandate as the ceiling.
  const ctx = useMemo(() => {
    switch (view) {
      case "agent":
        return { agentId: subjectId, runtimeId, userId, groupIds };
      case "runtime":
        return { runtimeId: subjectId };
      case "group":
        return { groupIds: [subjectId] };
      case "member":
        return { userId: subjectId };
      case "system":
        return { autopilotId: subjectId };
      case "workspace":
        return {};
    }
  }, [view, subjectId, runtimeId, userId, groupIds]);

  const query = useQuery(toolPolicyTableOptions(wsId, ctx));
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();

  const [search, setSearch] = useState("");
  const [types, setTypes] = useState<Set<PermissionType>>(new Set());
  const [decisions, setDecisions] = useState<Set<ToolEffectiveSetting>>(new Set());
  const [showInherited, setShowInherited] = useState(true);
  // Credential rows (FIR-1479) only exist once the workspace turns the feature
  // on; gate them on the same flag the backend gates the rows with so the
  // Credentials type is a consistent surface the moment an admin enables it.
  const showCredentials = useFeatureFlag("cerebro_credentials_per_actor");
  // FIR-2706: the redesigned "Tools & permissions" look — the flat catalog
  // rendered as grouped capability cards with inline-expanding connections and a
  // single Decision-toggle-that-holds-When per row. Presentation only; every
  // verdict, filter, and write path below is unchanged. Gated on the permissions
  // flag itself (`cerebro_tool_policy`, default ON) rather than the agent-page
  // preview — Jesper's call, so the catalog IS the default permissions surface on
  // production, and every surface that renders this shared component (agent Tools
  // tab, runtime, group, member, autopilot, connections, collections, settings)
  // shows it. The classic table below only serves installs with tool-policy off.
  const showCatalog = useFeatureFlag("cerebro_tool_policy");

  // The layer this page authors, and the subject those writes target.
  const editLayer: ToolLayer = VIEW_EDIT_LAYER[view];

  // The data-source scope binding (FIR-2083): which connection + tool fills the
  // When picker for an arg-scoped row. Cheap and shared; null when no connection
  // declares data-source scoping, in which case the arg picker simply never shows.
  const argScopeConfig = useDataSourceScopeConfig(wsId);

  const rows = query.data ?? [];
  // Capability-wide rows (the flat catalog) vs. per-resource rows. A per-resource
  // row carries a non-empty pattern: repos render as collapsible groups, while
  // connection tools (source "connection-tool") are surfaced through the
  // per-connection "Konfigurer" sheet — so they are excluded from the repo set.
  const allCapRows = useMemo(() => rows.filter((r) => !r.resource_pattern), [rows]);
  // Per-connection sub-rows (MCP tools or API endpoint+method) are surfaced
  // through the "Konfigurer" sheet, not as repo groups — so they're excluded
  // from the repo set.
  const isConnectionSubRow = (r: ToolPolicyRow) =>
    r.source === "connection-tool" || r.source === "connection-endpoint";
  // firtal_registry per-data-source rows (FIR-1609 Phase 5) carry a resource
  // pattern too, but — like connection sub-rows — they are surfaced through their
  // own "Data sources" sheet, not as repo groups, so exclude them from repos.
  const isRegistrySubRow = (r: ToolPolicyRow) => r.source === "registry-data-source";
  // Credential rows (FIR-1479) carry a resource pattern too ("cerebro-credential:<id>",
  // one row per credential capability), but they are their own "Credentials" tab — not
  // repos — so exclude them from the repo set the same way connection/registry sub-rows
  // are excluded.
  const isCredentialRow = (r: ToolPolicyRow) => r.source === "credential";
  const allRepoRows = useMemo(
    () =>
      rows.filter(
        (r) =>
          r.resource_pattern &&
          !isConnectionSubRow(r) &&
          !isRegistrySubRow(r) &&
          !isCredentialRow(r),
      ),
    [rows],
  );
  const allCredentialRows = useMemo(() => rows.filter(isCredentialRow), [rows]);
  // Registry data-source sub-rows grouped by their tool key ("firtal_registry"),
  // so the capability row can hand its data sources to the per-source sheet.
  const registryDataSourcesByKey = useMemo(() => {
    const map = new Map<string, ToolPolicyRow[]>();
    for (const r of rows) {
      if (!isRegistrySubRow(r)) continue;
      const list = map.get(r.tool_key) ?? [];
      list.push(r);
      map.set(r.tool_key, list);
    }
    return map;
  }, [rows]);
  // Connection sub-rows grouped by their connection capability key
  // ("connection:<name>") so each connection row can hand its tools to the sheet.
  const connectionToolsByKey = useMemo(() => {
    const map = new Map<string, ToolPolicyRow[]>();
    for (const r of rows) {
      if (!isConnectionSubRow(r)) continue;
      const list = map.get(r.tool_key) ?? [];
      list.push(r);
      map.set(r.tool_key, list);
    }
    return map;
  }, [rows]);

  // tabFilter splits the flade in three (FIR-2281, then FIR-2706). TanStack
  // Query deduplicates the underlying fetch, so the ToolPolicyTable instances
  // behind the tabs share a single network request.
  const capRows = useMemo(() => {
    // Connections tab: the only FLAT capability rows are the connection rows —
    // credentials render as collapsible sections (credentialGroups below), so
    // the flat table excludes them on this tab.
    if (tabFilter === "connections")
      return allCapRows.filter((r) => r.source === "connection");
    if (tabFilter === "permissions")
      // Everything that is not a connection — Multica + the runtime-reported tools.
      return allCapRows.filter((r) => r.source !== "connection");
    // Repos tab has no flat rows — repos render as collapsible sections only.
    if (tabFilter === "repos") return [];
    // No tabFilter: the single-table fallback shows every flat capability row.
    return allCapRows;
  }, [tabFilter, allCapRows]);

  const repoRows = useMemo(() => {
    // Repos belong to their own tab (and the no-filter fallback).
    if (!tabFilter || tabFilter === "repos") return allRepoRows;
    return [];
  }, [tabFilter, allRepoRows]);

  // The Type facets offered on this tab — only the types that actually have rows,
  // so an empty type never shows a dead filter chip.
  const typeFacets = useMemo<PermissionType[]>(() => {
    if (tabFilter === "permissions" || !tabFilter) {
      const present = new Set<PermissionType>();
      for (const r of capRows) present.add(permissionType(r));
      const order = tabFilter
        ? PERMISSION_TYPES_BY_TAB.permissions
        : [
            ...PERMISSION_TYPES_BY_TAB.permissions,
            ...PERMISSION_TYPES_BY_TAB.repos,
            ...PERMISSION_TYPES_BY_TAB.connections,
          ];
      return order.filter((t) => present.has(t));
    }
    if (tabFilter === "repos") {
      return allRepoRows.length ? ["Repos"] : [];
    }
    // connections tab
    const present: PermissionType[] = [];
    if (capRows.length) present.push("Connections");
    if (showCredentials && allCredentialRows.length) present.push("Credentials");
    return present;
  }, [tabFilter, capRows, allRepoRows, allCredentialRows, showCredentials]);

  const filtered = useMemo(
    () => filterRows(capRows, { search, types, decisions, showInherited, editLayer }),
    [capRows, search, types, decisions, showInherited, editLayer],
  );
  // Repo groups are keyed by URL and narrowed by the free-text search and the Type
  // filter (the "Repos" type) — never by the per-capability decision facet.
  const repoGroups = useMemo(
    () =>
      types.size && !types.has("Repos") ? [] : groupRepoRows(repoRows, search),
    [types, repoRows, search],
  );
  // Credential groups: one collapsible group per Agent Vault box, shown on the
  // Connections tab when the feature is on. Keyed by resource_pattern and narrowed
  // by the free-text search (on the box name) and the "Credentials" type filter.
  const credentialGroups = useMemo(() => {
    const onConnections = !tabFilter || tabFilter === "connections";
    if (!onConnections || !showCredentials) return [];
    if (types.size && !types.has("Credentials")) return [];
    return groupCredentialRows(allCredentialRows, search);
  }, [tabFilter, showCredentials, types, allCredentialRows, search]);

  const busy = setPolicy.isPending || clearPolicy.isPending;

  function applySetting(toolKey: string, setting: ToolSetting, resourcePattern?: string) {
    const scope = resourcePattern ? { resource_pattern: resourcePattern } : {};
    if (setting === "inherit") {
      clearPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId, ...scope });
      return;
    }
    setPolicy.mutate(
      { tool_key: toolKey, layer: editLayer, subject_id: subjectId, setting, ...scope },
      {
        // FIR-2706 follow-up: the server ACCEPTS a write a higher layer then
        // overrides, so the pill snapped back with no message — a silent
        // failure to the user. Rejected writes already toast via the hook's
        // onError; this covers the accepted-but-overridden case by naming the
        // blocking layer out loud the moment the write lands.
        onSuccess: () => {
          const row = rows.find(
            (r) =>
              r.tool_key === toolKey && (r.resource_pattern || "") === (resourcePattern || ""),
          );
          if (!row) return;
          const warning = futileWriteWarning(row, editLayer, setting);
          if (warning) toast.warning(warning, { duration: 8000 });
        },
      },
    );
  }

  // Write (or clear) the Condition — the WHEN layer (FIR-1609) — on this page's
  // own rule for one tool. A condition refines a rule, so it can only attach to a
  // CONCRETE rule (allow/ask/deny) already authored on this layer; the control is
  // disabled otherwise, and we re-guard here so a drifted row can never create an
  // override as a side effect. Passing null clears the condition ("always
  // applies"). The Decision is sent unchanged — a condition never moves it.
  function applyCondition(row: ToolPolicyRow, condition: ToolCondition | null) {
    const setting = editLayer === "group" ? null : row.layers[editLayer];
    if (setting !== "allow" && setting !== "ask" && setting !== "deny") return;
    const scope = row.resource_pattern ? { resource_pattern: row.resource_pattern } : {};
    setPolicy.mutate({
      tool_key: row.tool_key,
      layer: editLayer,
      subject_id: subjectId,
      setting,
      condition,
      ...scope,
    });
  }

  // Cascade one choice onto every capability of a repo (the group header
  // control): "set the repo to Allow and every row under it follows."
  function applyRepoGroup(group: RepoGroupData, setting: ToolSetting) {
    for (const row of group.rows) {
      applySetting(row.tool_key, setting, group.url);
    }
  }

  // Cascade one choice onto every capability of a credential box (the group
  // header control): "set bigquery to Allow and every action under it follows."
  function applyCredentialGroup(group: CredentialGroupData, setting: ToolSetting) {
    for (const row of group.rows) {
      applySetting(row.tool_key, setting, group.resource);
    }
  }

  function bulkSet(setting: Exclude<ToolSetting, "inherit">) {
    let skipped = 0;
    for (const row of filtered) {
      // TECH-3287 hul 6: "Allow all" can't loosen a row a higher layer blocks —
      // skip those instead of firing a silent dead write that reverts on refetch.
      if (setting === "allow" && isLockedFromElsewhere(row, editLayer)) {
        skipped += 1;
        continue;
      }
      setPolicy.mutate({ tool_key: row.tool_key, layer: editLayer, subject_id: subjectId, setting });
    }
    // Skipping silently looked like the bulk action half-failed (FIR-2706
    // follow-up) — say how many rows a higher layer holds locked.
    if (skipped > 0) {
      toast.info(
        skipped === 1
          ? "1 permission was left unchanged — a higher layer locks it. The lock icon on the row names the blocker."
          : `${skipped} permissions were left unchanged — higher layers lock them. The lock icon on each row names the blocker.`,
        { duration: 8000 },
      );
    }
  }

  // FIR-2706 — a catalog row is itself a GROUP when it carries underlying rows:
  // a connection with per-tool rows, or a tool (firtal_registry) with data
  // sources. Group rows expand inline instead of opening a Sheet, so the wide
  // "Configure (N)" / "Data sources (N)" buttons no longer crowd the row on mobile.
  const connToolsFor = (row: ToolPolicyRow) =>
    row.source === "connection" ? (connectionToolsByKey.get(row.tool_key) ?? []) : [];
  const dataSourcesFor = (row: ToolPolicyRow) =>
    registryDataSourcesByKey.get(row.tool_key) ?? [];

  // The per-row control in the redesigned catalog: ONE Decision toggle that also
  // holds the When editor inside its popover (FIR-2706 — Jesper's "When lives in
  // the toggle, not a second button on the bar"). Same single control for leaf and
  // group rows, so the row bar is always one control wide and the tool name never
  // gets crowded off on mobile. Group rows additionally expand (chevron) to reveal
  // their sub-tool list; that lives in renderCatalogDetail below.
  const renderDecision = (row: ToolPolicyRow) => (
    <CatalogDecisionControl
      row={row}
      editLayer={editLayer}
      disabled={busy}
      onDecision={(s) => applySetting(row.tool_key, s, row.resource_pattern || undefined)}
      onCondition={(c) => applyCondition(row, c)}
      wsId={wsId}
      argScopeConfig={argScopeConfig}
    />
  );

  // The inline detail for a group row: the per-tool list (connections) and data
  // sources as their own labelled sub-group — "expand and show the group"
  // (FIR-2706). The group-wide When now lives inside the row's Decision toggle
  // (see renderDecision), so it no longer sits at the top of the detail. Returns
  // null for a leaf row, which keeps its chevron hidden in the catalog.
  const renderCatalogDetail = (row: ToolPolicyRow): ReactNode | null => {
    const connTools = connToolsFor(row);
    const dataSources = dataSourcesFor(row);
    if (connTools.length === 0 && dataSources.length === 0) return null;
    return (
      <div className="flex flex-col gap-3">
        {connTools.length > 0 ? (
          <ConnectionToolList
            connectionKey={row.tool_key}
            connectionRow={row}
            toolRows={connTools}
            editLayer={editLayer}
            subjectId={subjectId}
          />
        ) : null}
        {dataSources.length > 0 ? (
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-semibold text-muted-foreground">
              Data sources
            </span>
            <DataSourceList
              toolKey={row.tool_key}
              sourceRows={dataSources}
              editLayer={editLayer}
              subjectId={subjectId}
            />
          </div>
        ) : null}
      </div>
    );
  };

  return (
    <div className="flex flex-col gap-4" data-testid="tool-policy-table">
      <CatalogHeader
        shown={filtered.length}
        total={
          capRows.length +
          repoRows.length +
          (showCredentials && (!tabFilter || tabFilter === "connections")
            ? allCredentialRows.length
            : 0)
        }
        busy={busy}
        onBulk={bulkSet}
      />

      <FilterBar
        search={search}
        onSearch={setSearch}
        typeFacets={typeFacets}
        types={types}
        onToggleType={(t) => setTypes((s) => toggle(s, t))}
        decisions={decisions}
        onToggleDecision={(d) => setDecisions((s) => toggle(s, d))}
        showInherited={showInherited}
        onShowInherited={setShowInherited}
        editLayerLabel={LAYER_LABEL[editLayer]!}
      />

      {query.isLoading ? (
        <p className="text-sm text-muted-foreground">Loading tools…</p>
      ) : repoGroups.length === 0 && credentialGroups.length === 0 && filtered.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {allCapRows.length === 0 && allRepoRows.length === 0 && allCredentialRows.length === 0
            ? "No tools reported yet."
            : "No tools match these filters."}
        </p>
      ) : (
        <>
          {repoGroups.length > 0 && (
            <RepoSection
              groups={repoGroups}
              editLayer={editLayer}
              busy={busy}
              onSetCapability={(url, toolKey, s) => applySetting(toolKey, s, url)}
              onSetGroup={applyRepoGroup}
              onSetCondition={applyCondition}
              wsId={wsId}
              argScopeConfig={argScopeConfig}
            />
          )}

          {credentialGroups.length > 0 && (
            <CredentialSection
              groups={credentialGroups}
              editLayer={editLayer}
              busy={busy}
              onSetCapability={(resource, toolKey, s) => applySetting(toolKey, s, resource)}
              onSetGroup={applyCredentialGroup}
              onSetCondition={applyCondition}
              wsId={wsId}
              argScopeConfig={argScopeConfig}
            />
          )}

          {filtered.length > 0 && showCatalog && (
            <CapabilityCatalog
              rows={filtered}
              renderDecision={renderDecision}
              renderDetail={renderCatalogDetail}
            />
          )}

          {filtered.length > 0 && !showCatalog && (
          <>
          {/* Desktop: the full sortable catalog table. */}
          <div className="hidden overflow-hidden rounded-lg border md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[38%]">Tool</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Decision</TableHead>
                  <TableHead>Origin</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((row) => (
                  <TableRow
                    key={`${row.tool_key}:${row.resource_pattern ?? ""}`}
                    data-testid={`tool-row-${row.tool_key}${
                      row.resource_pattern ? `:${row.resource_pattern}` : ""
                    }`}
                  >
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="flex items-center gap-1 font-medium">
                          {row.title || row.tool_key}
                          <PermissionHelp row={row} />
                        </span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {row.tool_key}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <TypeTag row={row} />
                        {row.managed_externally && <ManagedExternallyTag />}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <DecisionControl
                          row={row}
                          editLayer={editLayer}
                          disabled={busy}
                          onChange={(s) => applySetting(row.tool_key, s, row.resource_pattern || undefined)}
                        />
                        <ConditionControl
                          row={row}
                          editLayer={editLayer}
                          disabled={busy}
                          onChange={(c) => applyCondition(row, c)}
                          wsId={wsId}
                          argScopeConfig={argScopeConfig}
                        />
                        {view === "agent" && subjectId ? (
                          <FirtalRegistryRowConfigure
                            toolKey={row.tool_key}
                            agentId={subjectId}
                            variant="outline"
                          />
                        ) : null}
                        {row.source === "connection" ? (
                          <ConnectionRowConfigure
                            connectionKey={row.tool_key}
                            connectionLabel={row.title || row.tool_key}
                            toolRows={connectionToolsByKey.get(row.tool_key) ?? []}
                            connectionRow={row}
                            editLayer={editLayer}
                            subjectId={subjectId}
                          />
                        ) : null}
                        <FirtalRegistryDataSourceConfigure
                          toolKey={row.tool_key}
                          toolLabel={row.title || row.tool_key}
                          sourceRows={registryDataSourcesByKey.get(row.tool_key) ?? []}
                          editLayer={editLayer}
                          subjectId={subjectId}
                        />
                      </div>
                    </TableCell>
                    <TableCell>
                      <OriginTag row={row} editLayer={editLayer} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          {/* Mobile: the same catalog as stacked cards. */}
          <div className="flex flex-col gap-2 md:hidden">
            {filtered.map((row) => (
              <div
                key={`${row.tool_key}:${row.resource_pattern ?? ""}`}
                data-testid={`tool-card-${row.tool_key}${
                  row.resource_pattern ? `:${row.resource_pattern}` : ""
                }`}
                className="rounded-lg border p-3"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-1 truncate text-sm font-medium">
                      {row.title || row.tool_key}
                      <PermissionHelp row={row} />
                    </div>
                    <div className="truncate font-mono text-xs text-muted-foreground">
                      {row.tool_key}
                    </div>
                  </div>
                  {/* flex-col-reverse stacks the controls vertically on mobile
                      with the Decision pill (last child) on TOP and the
                      Configure / Data sources buttons UNDER it, so the wide
                      buttons no longer steal the row's width and the capability
                      name stays readable (FIR-2640 review). */}
                  <div className="flex shrink-0 flex-col-reverse items-end gap-1.5">
                    {view === "agent" && subjectId ? (
                      <FirtalRegistryRowConfigure
                        toolKey={row.tool_key}
                        agentId={subjectId}
                        variant="outline"
                      />
                    ) : null}
                    {row.source === "connection" ? (
                      <ConnectionRowConfigure
                        connectionKey={row.tool_key}
                        connectionLabel={row.title || row.tool_key}
                        toolRows={connectionToolsByKey.get(row.tool_key) ?? []}
                            connectionRow={row}
                        editLayer={editLayer}
                        subjectId={subjectId}
                      />
                    ) : null}
                    <FirtalRegistryDataSourceConfigure
                      toolKey={row.tool_key}
                      toolLabel={row.title || row.tool_key}
                      sourceRows={registryDataSourcesByKey.get(row.tool_key) ?? []}
                      editLayer={editLayer}
                      subjectId={subjectId}
                    />
                    <DecisionControl
                      row={row}
                      editLayer={editLayer}
                      disabled={busy}
                      onChange={(s) => applySetting(row.tool_key, s, row.resource_pattern || undefined)}
                    />
                  </div>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  <TypeTag row={row} />
                  {row.managed_externally && <ManagedExternallyTag />}
                  <OriginTag row={row} editLayer={editLayer} />
                  <ConditionControl
                    row={row}
                    editLayer={editLayer}
                    disabled={busy}
                    onChange={(c) => applyCondition(row, c)}
                    wsId={wsId}
                    argScopeConfig={argScopeConfig}
                  />
                </div>
              </div>
            ))}
          </div>
          </>
          )}
        </>
      )}
    </div>
  );
}

export function ToolPolicyTabs(props: ToolPolicyTabsProps) {
  // FIR-2281 introduced two tabs instead of five. FIR-2706 splits the former
  // "Resources" tab in two again: "Repos" and "Connections" now each get their
  // own dedicated tab instead of being told apart only by the Type column and
  // filter inside a shared table. "Permissions" is unchanged — the flat
  // capabilities (Multica + Runtime). Credential boxes live under
  // "Connections" (see PERMISSION_TYPES_BY_TAB); the credentials feature flag
  // is read inside the table itself, so it never has to gate a whole tab here.
  return (
    // TECH-3156 Mangel 3: force the tab row horizontal. The shared Tabs primitive
    // renders its list vertically by default, so — like cost-optimization-tabs —
    // we override the list to !flex-row and each trigger to !w-auto so the tabs sit
    // on one horizontal row instead of stacked. On narrow screens the row scrolls
    // horizontally (flex-nowrap + overflow-x-auto) instead of wrapping and breaking
    // over the content below it.
    <Tabs defaultValue="permissions" orientation="horizontal">
      <TabsList className="no-scrollbar !h-auto w-full max-w-full !flex-row flex-nowrap justify-start gap-1 overflow-x-auto">
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="permissions">
          Permissions
        </TabsTrigger>
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="repos">
          Repos
        </TabsTrigger>
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="connections">
          Connections
        </TabsTrigger>
      </TabsList>
      <TabsContent value="permissions" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="permissions" />
      </TabsContent>
      <TabsContent value="repos" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="repos" />
      </TabsContent>
      <TabsContent value="connections" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="connections" />
      </TabsContent>
    </Tabs>
  );
}

// --- filtering --------------------------------------------------------------

interface FilterState {
  search: string;
  types: Set<PermissionType>;
  decisions: Set<ToolEffectiveSetting>;
  showInherited: boolean;
  editLayer: ToolLayer;
}

// filterRows applies the combinable filters. Each facet (type / decision) is OR
// within itself and AND across facets; an empty set means "all". "Show inherited"
// off keeps only rows this page has explicitly authored at its own layer, so a
// reviewer can see just the overrides they own.
export function filterRows(rows: ToolPolicyRow[], f: FilterState): ToolPolicyRow[] {
  const q = f.search.trim().toLowerCase();
  return rows.filter((r) => {
    if (q && !`${r.title} ${r.tool_key} ${r.category}`.toLowerCase().includes(q)) {
      return false;
    }
    if (f.types.size && !f.types.has(permissionType(r))) return false;
    if (f.decisions.size && !f.decisions.has(r.effective.setting)) return false;
    if (!f.showInherited && !r.layers[f.editLayer]) return false;
    return true;
  });
}

function toggle<T>(set: Set<T>, value: T): Set<T> {
  const next = new Set(set);
  if (next.has(value)) next.delete(value);
  else next.add(value);
  return next;
}

// --- header -----------------------------------------------------------------

function CatalogHeader({
  shown,
  total,
  busy,
  onBulk,
}: {
  shown: number;
  total: number;
  busy: boolean;
  onBulk: (setting: "allow" | "deny") => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-base font-semibold">All tools</h3>
          <p className="max-w-xl text-sm text-muted-foreground">
            Permissions attach at the tool level; filter by type or decision.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground">
            {shown === total ? `${total} tools` : `${shown} of ${total} tools`}
          </span>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => onBulk("allow")}>
            Allow all
          </Button>
          <Button size="sm" variant="outline" disabled={busy} onClick={() => onBulk("deny")}>
            Deny all
          </Button>
        </div>
      </div>
    </div>
  );
}

// --- filter bar -------------------------------------------------------------

function FilterBar({
  search,
  onSearch,
  typeFacets,
  types,
  onToggleType,
  decisions,
  onToggleDecision,
  showInherited,
  onShowInherited,
  editLayerLabel,
}: {
  search: string;
  onSearch: (v: string) => void;
  typeFacets: PermissionType[];
  types: Set<PermissionType>;
  onToggleType: (t: PermissionType) => void;
  decisions: Set<ToolEffectiveSetting>;
  onToggleDecision: (d: ToolEffectiveSetting) => void;
  showInherited: boolean;
  onShowInherited: (v: boolean) => void;
  editLayerLabel: string;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative w-full sm:max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder="Filter tools by name…"
            className="h-9 pl-9"
            aria-label="Filter tools"
          />
        </div>
      </div>

      {typeFacets.length > 1 && (
        <FilterGroup label="Type">
          {typeFacets.map((t) => (
            <FilterChip key={t} active={types.has(t)} onClick={() => onToggleType(t)}>
              {t}
            </FilterChip>
          ))}
        </FilterGroup>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <FilterGroup label="Decision">
          {DECISION_FILTERS.map((d) => (
            <FilterChip
              key={d}
              active={decisions.has(d)}
              onClick={() => onToggleDecision(d)}
            >
              {SETTING_LABEL[d]}
            </FilterChip>
          ))}
        </FilterGroup>
        <label className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
          <Checkbox
            checked={showInherited}
            onCheckedChange={(v) => onShowInherited(v === true)}
            aria-label="Show inherited"
          />
          Show inherited
          <span className="text-xs">(off = only {editLayerLabel} overrides)</span>
        </label>
      </div>
    </div>
  );
}

function FilterGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      {children}
    </div>
  );
}

function FilterChip({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors",
        active
          ? "border-primary bg-primary/10 text-foreground"
          : "border-border bg-background text-muted-foreground hover:bg-muted",
      )}
    >
      {children}
    </button>
  );
}

// --- row cells --------------------------------------------------------------

// ManagedExternallyTag marks a platform action whose access is decided by
// another mechanism (membership ACL, daemon token, webhook secret), so its
// Allow/Ask/Deny choice here is advisory rather than the enforcement point
// (FIR-2594). Shown so an admin sees the platform exposes the action.
function ManagedExternallyTag() {
  return (
    <Badge
      variant="outline"
      className="border-dashed font-normal text-muted-foreground"
      title="Access is governed outside the tool-policy gate (membership, daemon token, or webhook secret). Listed for visibility."
    >
      Managed externally
    </Badge>
  );
}

// PermissionHelp shows a small info icon next to a permission, carrying the
// plain-language explanation of what the permission does as a native tooltip
// (FIR-2175 phase 3) — so the table is self-explanatory to a non-technical
// operator. The text follows the active UI language (Chinese for a zh* locale
// when a translation exists, English otherwise). Renders nothing when the
// capability has no catalogued description, so an un-described row is unchanged.
function PermissionHelp({ row }: { row: ToolPolicyRow }) {
  const { i18n } = useTranslation();
  const text = permissionDescription(row, i18n.language);
  if (!text) return null;
  return (
    <span
      title={text}
      aria-label={text}
      className="inline-flex shrink-0 cursor-help text-muted-foreground"
    >
      <Info className="size-3.5" aria-hidden />
    </span>
  );
}

// originOf frames one row relative to the level THIS page authors (editLayer):
// either the rule is an override set right here, or it is inherited and we name
// the level it actually comes from. This is the FIR-2284 "override vs. arv per
// row" requirement — every row must say whether it is an override on the current
// level or inherited from another, and which level set it.
export function originOf(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
): { kind: "override" | "inherited"; level: string; label: string } {
  // An explicit setting at this page's own layer (incl. a deliberate "inherit"
  // pass-through) is an override authored here.
  if (row.layers[editLayer]) {
    const level = LAYER_LABEL[editLayer] ?? editLayer;
    return { kind: "override", level, label: `Override on ${level}` };
  }
  // Otherwise the verdict was decided elsewhere in the chain. "" = no layer had
  // an opinion, so the workspace default at the root decided.
  const decided = row.effective.decided_by;
  const level = decided ? (LAYER_LABEL[decided] ?? decided) : "Workspace default";
  return { kind: "inherited", level, label: `Inherited from ${level}` };
}

// changeHint is the navigation breadcrumb shown in the tooltip so an admin knows
// WHERE to change a blocking layer, not just that it is blocked (TECH-3287 hul 4).
function changeHint(layer: string): string {
  switch (layer) {
    case "workspace":
      return "Change it under Settings → Tools (workspace).";
    case "runtime":
      return "Change it under Runtime settings → Tools.";
    case "agent":
      return "Change it on this agent's Tools tab.";
    case "group":
      return "Change it under Settings → Groups.";
    case "user":
      return "It is set on the user's own permissions (the ceiling).";
    case "system":
      return "It is set on the autopilot's System ceiling (select the autopilot above).";
    default:
      return "";
  }
}

// formatGroupAttribution renders the blocking group(s) as "Navn (ejer: Person)",
// the TECH-3287 hul 5 copy. Owner is omitted when the backend has no creator.
function formatGroupAttribution(row: ToolPolicyRow): string {
  return row.capped_by_groups
    .map((g) => (g.owner ? `${g.name} (owner: ${g.owner})` : g.name))
    .join(", ");
}

interface RowAttribution {
  kind: "override" | "inherited" | "capped";
  label: string;
  tooltip: string;
}

// blockerText names the layer (or group, by name + owner) that forces the
// verdict, for the "where to change it" copy (TECH-3287 hul 4/5).
function blockerText(row: ToolPolicyRow): { phrase: string; hint: string } {
  const blocker = row.effective.capped_by || row.effective.decided_by;
  if (blocker === "group" && row.capped_by_groups.length > 0) {
    return { phrase: `group ${formatGroupAttribution(row)}`, hint: changeHint("group") };
  }
  return { phrase: LAYER_LABEL[blocker] ?? blocker, hint: changeHint(blocker) };
}

// futileWriteWarning — the message to toast when a write the server ACCEPTED
// is nonetheless overridden by a layer this page cannot loosen (FIR-2706
// follow-up: an accepted-but-capped write used to revert with no explanation
// beyond a hover tooltip). Returns null when the write actually takes effect:
// tightening (deny/disable) always bites, and so does any choice at or above
// the effective verdict's restrictiveness. Exported for unit tests.
export function futileWriteWarning(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
  setting: ToolSetting,
): string | null {
  if (setting !== "allow" && setting !== "ask") return null;
  if (!isLockedFromElsewhere(row, editLayer)) return null;
  const effective = row.effective.setting;
  if (SETTING_RANK[setting] >= SETTING_RANK[effective]) return null;
  const { phrase, hint } = blockerText(row);
  return `"${SETTING_LABEL[setting]}" was saved on the ${LAYER_LABEL[editLayer]} layer, but it has no effect: the decision stays "${SETTING_LABEL[effective]}" because ${phrase} blocks it. ${hint}`.trim();
}

// rowAttribution is the single source of truth for what the Origin badge says.
//
// TECH-3287 hul 2/4/5 reframes two things WITHOUT touching the established
// override/inherited language for the normal case:
//   1. The lie: when this page holds an explicit override that a HIGHER layer
//      overrides (a futile override beneath a cap), the old code still printed
//      "Override on Agent". We now name the real blocker instead.
//   2. The silent inherit: when a row is locked from a layer this page can't
//      loosen, we keep the "Inherited from X" label but enrich the tooltip with
//      where to change the blocking layer (and the DecisionControl shows a lock).
export function rowAttribution(row: ToolPolicyRow, editLayer: ToolLayer): RowAttribution {
  const locked = isLockedFromElsewhere(row, editLayer);
  const hasLocalOverride = !!row.layers[editLayer];
  const blocker = row.effective.capped_by || row.effective.decided_by;

  // Case 1 — a futile local override beneath a higher decision: tell the truth.
  if (locked && hasLocalOverride && blocker !== editLayer) {
    const { phrase, hint } = blockerText(row);
    return {
      kind: "capped",
      label: blocker === "group" ? `Capped by group ${formatGroupAttribution(row)}` : `Capped by ${phrase}`,
      tooltip: `Your ${LAYER_LABEL[editLayer]} setting has no effect — blocked by ${phrase}. ${hint}`,
    };
  }

  // Case 2 — the normal override/inherited language, enriched when locked.
  const origin = originOf(row, editLayer);
  let tooltip =
    origin.kind === "override"
      ? `This rule is an override set on ${origin.level}.`
      : `No override on this level — the rule is inherited from ${origin.level}.`;
  if (locked) {
    const { phrase, hint } = blockerText(row);
    tooltip = `Blocked by ${phrase} — cannot be made more open here. ${hint}`;
  }
  return { kind: origin.kind, label: origin.label, tooltip };
}

function OriginTag({
  row,
  editLayer,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
}) {
  const attr = rowAttribution(row, editLayer);
  return (
    <Badge
      variant="outline"
      title={attr.tooltip}
      className={cn(
        "font-normal",
        attr.kind === "override" && "border-primary/40 text-primary",
        attr.kind === "capped" && "border-destructive/40 text-destructive",
        attr.kind === "inherited" && "text-muted-foreground",
      )}
    >
      {attr.label}
    </Badge>
  );
}

// DecisionControl is the single editable pill the redesign settled on (one clear
// control per row, not a four-button badge soup). The pill shows the EFFECTIVE
// verdict — what actually happens — coloured and with a Lock icon when a layer
// above this page capped it. Opening it writes this page's own layer; choosing
// Inherit clears that layer. A subtle ring marks rows this page has overridden.
export function DecisionControl({
  row,
  editLayer,
  disabled,
  onChange,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
  disabled?: boolean;
  onChange: (setting: ToolSetting) => void;
}) {
  const verdict = row.effective.setting;
  // Lock whenever the verdict is forced by a layer this page can't loosen — not
  // only the group/user cap the old code checked, but also a workspace/runtime
  // base Deny on the agent page (TECH-3287 hul 2).
  const locked = isLockedFromElsewhere(row, editLayer);
  const Icon = locked ? Lock : VERDICT_ICON[verdict];
  const overridden = !!row.layers[editLayer];
  const lockTooltip = locked ? rowAttribution(row, editLayer).tooltip : undefined;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={`Decision: ${SETTING_LABEL[verdict]}`}
        title={lockTooltip}
        className={cn(
          "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold transition-colors disabled:opacity-50",
          VERDICT_PILL[verdict],
          overridden && "ring-1 ring-primary/40",
        )}
      >
        <Icon className="size-3.5" />
        {SETTING_LABEL[verdict]}
        <ChevronDown className="size-3 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-36">
        {/* FIR-2706 follow-up: the lock explanation lived only in a hover
            tooltip, so a capped row read as silently broken. Show the reason
            in the menu itself, right where the user is about to choose. */}
        {locked && lockTooltip ? (
          <div className="flex max-w-64 items-start gap-1.5 border-b px-2 py-1.5 text-xs text-muted-foreground">
            <Lock className="mt-0.5 size-3 shrink-0" />
            <span>{lockTooltip}</span>
          </div>
        ) : null}
        {settingChoicesFor(editLayer).map((choice) => (
          <DropdownMenuItem
            key={choice}
            onClick={() => onChange(choice)}
            className={cn(
              "text-sm",
              choice === "inherit" && "text-muted-foreground",
              row.layers[editLayer] === choice && "font-semibold",
            )}
          >
            {SETTING_LABEL[choice]}
            {choice === "inherit" && (
              <span className="ml-auto text-xs text-muted-foreground">clears {LAYER_LABEL[editLayer]}</span>
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// CatalogDecisionControl is the SINGLE per-row control for the redesigned catalog
// (FIR-2706): one pill on the row that opens a popover holding BOTH the decision
// choices (Allow/Ask/Deny/Inherit) AND the When editor. This is Jesper's ask —
// "the When lives inside the Allow toggle, not as a second button on the bar" — so
// the row bar stays one control wide and the tool name never gets crowded off on
// mobile. It composes the same DecisionControl verdict styling and the extracted
// ConditionEditorBody, so the underlying write semantics are unchanged.
//
// Exported for the inline group lists (ConnectionToolList, DataSourceList) so
// every expanded sub-row in the catalog renders the exact same control
// (FIR-2706 follow-up — Jesper: "100% identical everywhere"). The optional
// floorRank/floorLabel pair carries the connection sheet's tighten-only rule:
// choices looser than the group-wide floor are disabled as futile
// (TECH-3287 hul 7).
export function CatalogDecisionControl({
  row,
  editLayer,
  disabled,
  onDecision,
  onCondition,
  wsId,
  argScopeConfig,
  floorRank,
  floorLabel,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
  disabled?: boolean;
  onDecision: (setting: ToolSetting) => void;
  onCondition: (condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
  /** Restrictiveness rank of the group-wide floor; looser choices are futile. */
  floorRank?: number;
  /** Display label of the group-wide floor, for the futile-choice tooltip. */
  floorLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const verdict = row.effective.setting;
  const locked = isLockedFromElsewhere(row, editLayer);
  const Icon = locked ? Lock : VERDICT_ICON[verdict];
  const overridden = !!row.layers[editLayer];
  const lockTooltip = locked ? rowAttribution(row, editLayer).tooltip : undefined;

  // Does this row have a meaningful When condition to edit? Mirrors
  // ConditionControl's gate so the When section shows exactly where it would as a
  // standalone control — but now inside the same popover as the decision.
  const ruleSetting = editLayer === "group" ? null : row.layers[editLayer];
  const hasConcreteRule =
    ruleSetting === "allow" || ruleSetting === "ask" || ruleSetting === "deny";
  const current = conditionForLayer(row, editLayer);
  const active = !conditionIsEmpty(current);
  const facets = conditionFacets(row);
  const showGroupArg = facets.arg && row.tool_key === "manage_group_overrides" && !!wsId;
  const showArg =
    facets.arg && row.tool_key !== "manage_group_overrides" && !!wsId && !!argScopeConfig;
  const meaningful =
    facets.host || facets.actions.length > 0 || facets.cel || showArg || showGroupArg;
  const showWhen = editLayer !== "group" && (meaningful || active);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        aria-label={`Decision: ${SETTING_LABEL[verdict]}`}
        title={lockTooltip}
        className={cn(
          "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold transition-colors disabled:opacity-50",
          VERDICT_PILL[verdict],
          overridden && "ring-1 ring-primary/40",
        )}
      >
        <Icon className="size-3.5" />
        {SETTING_LABEL[verdict]}
        {active ? (
          <SlidersHorizontal className="size-3 opacity-70" aria-label="Has a When condition" />
        ) : null}
        <ChevronDown className="size-3 opacity-60" />
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 p-0">
        <div className="flex flex-col">
          {/* FIR-2706 follow-up: same visible lock explanation as
              DecisionControl — a hover-only tooltip read as a silent failure. */}
          {locked && lockTooltip ? (
            <div className="flex items-start gap-1.5 border-b px-3 py-2 text-xs text-muted-foreground">
              <Lock className="mt-0.5 size-3 shrink-0" />
              <span>{lockTooltip}</span>
            </div>
          ) : null}
          <div className="flex flex-col gap-0.5 p-1.5">
            <span className="px-2 pb-0.5 pt-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
              Decision
            </span>
            {settingChoicesFor(editLayer).map((choice) => {
              // "disable" (FIR-2351 follow-up) is never futile: it is always at
              // least as restrictive as the tightest connection floor (Deny), so
              // the below-floor-rank futile check only applies to allow/ask/deny.
              const futile =
                floorRank !== undefined &&
                choice !== "inherit" &&
                choice !== "disable" &&
                SETTING_RANK[choice] < floorRank;
              return (
                <button
                  key={choice}
                  type="button"
                  data-testid={`catalog-decision-${row.tool_key}-${choice}`}
                  disabled={futile}
                  title={
                    futile
                      ? `The connection is set to “${floorLabel}” — “${SETTING_LABEL[choice]}” here is more open and has no effect.`
                      : undefined
                  }
                  onClick={() => {
                    onDecision(choice);
                    setOpen(false);
                  }}
                  className={cn(
                    "flex items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted",
                    choice === "inherit" && "text-muted-foreground",
                    row.layers[editLayer] === choice && "font-semibold",
                    futile && "cursor-not-allowed opacity-50 hover:bg-transparent",
                  )}
                >
                  {SETTING_LABEL[choice]}
                  {choice === "inherit" && (
                    <span className="ml-auto text-xs text-muted-foreground">
                      clears {LAYER_LABEL[editLayer]}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
          {showWhen ? (
            <div className="border-t p-3">
              {hasConcreteRule ? (
                <ConditionEditorBody
                  row={row}
                  editLayer={editLayer}
                  onChange={onCondition}
                  wsId={wsId}
                  argScopeConfig={argScopeConfig}
                  enabled={open}
                  onClose={() => setOpen(false)}
                />
              ) : (
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <SlidersHorizontal className="size-3.5 shrink-0" />
                  Set a decision above to add a When condition.
                </p>
              )}
            </div>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  );
}

// --- condition editor (FIR-1609 — the WHEN layer) ---------------------------

// Bare host or subdomain wildcard, e.g. firtal.com or *.firtal.com. Mirrors the
// server-side rule grammar (webfetchpolicy.HostMatchesRule) so the host-allowlist
// condition speaks the exact same language as the standalone Web fetch policy it
// folds in.
const HOST_RULE_RE = /^(\*\.)?[a-z0-9-]+(\.[a-z0-9-]+)+$/;

function normalizeHost(raw: string): string {
  return raw.trim().toLowerCase().replace(/^https?:\/\//, "").replace(/\/.*$/, "");
}

// A condition with nothing in it means "always applies" — there is no rule to
// refine, so it is treated as no condition (and cleared to NULL server-side).
export function conditionIsEmpty(c: ToolCondition | null | undefined): boolean {
  if (!c) return true;
  const argEmpty =
    !c.arg_allowlist || c.arg_allowlist.every((a) => a.values.length === 0);
  return (
    c.host_allowlist.length === 0 &&
    c.actions.length === 0 &&
    argEmpty &&
    c.expr.trim() === ""
  );
}

// argSummary describes a non-empty arg-allowlist for the trigger label, e.g.
// "Finance +1" for a folder scope or "3 sources" for specific-source scope.
function argSummary(list: ToolCondition["arg_allowlist"]): string | null {
  const entry = (list ?? []).find((a) => a.values.length > 0);
  if (!entry) return null;
  const n = entry.values.length;
  if (entry.arg === GROUP_ARG) return n === 1 ? "1 group" : `${n} groups`;
  if (entry.arg === FOLDER_ARG) return n === 1 ? "1 folder" : `${n} folders`;
  return n === 1 ? "1 source" : `${n} sources`;
}

// The Condition lives only on single-subject layers. The Group layer is a
// combined value across several group rules, for which a single condition is
// undefined — so the control is not shown there (mirrors ToolPolicyConditions).
function conditionForLayer(
  row: ToolPolicyRow,
  editLayer: ToolLayer,
): ToolCondition | null {
  if (editLayer === "group") return null;
  return row.conditions?.[editLayer] ?? null;
}

// summarizeCondition renders a non-empty condition as a compact trigger label:
// "firtal.com +1 · 2 actions · CEL".
export function summarizeCondition(c: ToolCondition): string {
  const parts: string[] = [];
  if (c.host_allowlist.length > 0) {
    parts.push(
      c.host_allowlist.length === 1
        ? c.host_allowlist[0]!
        : `${c.host_allowlist[0]} +${c.host_allowlist.length - 1}`,
    );
  }
  if (c.actions.length > 0) {
    parts.push(c.actions.length === 1 ? c.actions[0]! : `${c.actions.length} actions`);
  }
  const arg = argSummary(c.arg_allowlist);
  if (arg) parts.push(arg);
  if (c.expr.trim()) parts.push("CEL");
  return parts.join(" · ");
}

function ConditionChips({
  items,
  onRemove,
}: {
  items: string[];
  onRemove: (value: string) => void;
}) {
  if (items.length === 0) return null;
  return (
    <ul className="flex flex-wrap gap-1.5">
      {items.map((it) => (
        <li key={it}>
          <Badge variant="secondary" className="gap-1 py-0.5 pl-2 pr-1 font-mono text-xs">
            {it}
            <button
              type="button"
              aria-label={`Remove ${it}`}
              onClick={() => onRemove(it)}
              className="rounded-sm p-0.5 text-muted-foreground hover:bg-muted-foreground/20 hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          </Badge>
        </li>
      ))}
    </ul>
  );
}

// ConditionControl is the CONTEXTUAL WHEN editor that sits beside the Decision
// pill. A condition NEVER changes Allow/Ask/Deny — it only narrows whether this
// layer's rule applies to a request (host allow-list, allowed actions, or a CEL
// escape hatch for genuine dynamics). It can therefore only attach to a CONCRETE
// rule already authored on this page's own layer.
//
// The CEO rejected a generic editor that showed a host allow-list and a free-text
// actions box on EVERY tool (a host allow-list on a notification tool is
// nonsense). So the editor is now driven by conditionFacets(row): the host
// section shows only when the tool egresses to a host, the Actions section shows
// only when the tool has a verb dimension — and as PRESET TOGGLE CHIPS, not a
// free-text input — and the CEL textarea lives under a collapsed Advanced
// disclosure. On a row where neither facet is meaningful AND no condition is
// already set, the whole control is hidden — there is no "When" affordance on a
// tool where a condition makes no sense. The Group layer has no single condition,
// so the control is omitted entirely on that page.
export function ConditionControl({
  row,
  editLayer,
  disabled,
  onChange,
  wsId,
  argScopeConfig,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
  disabled?: boolean;
  onChange: (condition: ToolCondition | null) => void;
  /** Workspace id, needed to fetch scope picker options (FIR-2083). */
  wsId?: string;
  /** The data-source scope binding for an arg-scoped row, if one applies. */
  argScopeConfig?: ScopeConfig | null;
}) {
  const [open, setOpen] = useState(false);

  const ruleSetting = editLayer === "group" ? null : row.layers[editLayer];
  const hasConcreteRule =
    ruleSetting === "allow" || ruleSetting === "ask" || ruleSetting === "deny";
  const current = conditionForLayer(row, editLayer);
  const active = !conditionIsEmpty(current);
  // The contextual facets: which structured sections are worth showing for this
  // tool. `meaningful` is the gate for whether the control appears at all.
  const facets = conditionFacets(row);
  const showArg = facets.arg && !!wsId && !!argScopeConfig;
  const meaningful =
    facets.host || facets.actions.length > 0 || facets.cel || showArg;

  // Group has no single condition — show nothing there.
  if (editLayer === "group") return null;
  // Hide entirely on a tool where a condition makes no sense AND none is set — no
  // stray "When" affordance on a notification tool. A row that already carries a
  // condition keeps its control so the value stays editable.
  if (!meaningful && !active) return null;

  // No concrete rule on this layer → nothing to refine. Disabled hint that names
  // the prerequisite, rather than silently creating an override.
  if (!hasConcreteRule) {
    return (
      <button
        type="button"
        disabled
        data-testid={`condition-control-${row.tool_key}`}
        aria-label="Condition unavailable"
        title="Set a decision (Allow/Ask/Deny) on this level first to add a condition."
        className="inline-flex items-center gap-1 rounded-md border border-dashed px-2 py-1 text-xs font-medium text-muted-foreground opacity-50"
      >
        <SlidersHorizontal className="size-3.5" />
        When
      </button>
    );
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        disabled={disabled}
        data-testid={`condition-control-${row.tool_key}`}
        aria-label={active ? `Condition: ${summarizeCondition(current!)}` : "Add condition"}
        title={
          active
            ? `Applies only when: ${summarizeCondition(current!)}`
            : "Add a condition — narrow when this rule applies"
        }
        className={cn(
          "inline-flex max-w-[12rem] items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-50",
          active
            ? "border-primary/40 bg-primary/10 text-primary"
            : "border-dashed border-border bg-background text-muted-foreground hover:bg-muted",
        )}
      >
        <SlidersHorizontal className="size-3.5 shrink-0" />
        <span className="truncate">{active ? summarizeCondition(current!) : "When…"}</span>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80">
        <ConditionEditorBody
          row={row}
          editLayer={editLayer}
          onChange={onChange}
          wsId={wsId}
          argScopeConfig={argScopeConfig}
          enabled={open}
          onClose={() => setOpen(false)}
        />
      </PopoverContent>
    </Popover>
  );
}

// ConditionEditorBody is the WHEN editor form (FIR-2706), extracted from
// ConditionControl so it renders both in that standalone popover AND inside the
// catalog Decision toggle's popover — so "When" no longer needs a second button on
// the row bar (the mobile-crowding fix). It owns its draft state, seeded from the
// persisted condition each time `enabled` rises. Callers MUST gate that a concrete
// rule exists (hasConcreteRule) before showing it.
export function ConditionEditorBody({
  row,
  editLayer,
  onChange,
  wsId,
  argScopeConfig,
  enabled,
  onClose,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
  onChange: (condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
  /** True while the editor is visible; gates seeding + the lazy scope fetch. */
  enabled: boolean;
  /** Close the surrounding popover after Save / Cancel / Clear. */
  onClose: () => void;
}) {
  const [hosts, setHosts] = useState<string[]>([]);
  const [actions, setActions] = useState<string[]>([]);
  const [expr, setExpr] = useState("");
  const [hostDraft, setHostDraft] = useState("");
  const [hostError, setHostError] = useState<string | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  // Argument-value allowlist picker state (FIR-2083). `argMode` chooses the
  // scoping axis: "folder" writes a folder_id allowlist that auto-covers sources
  // added to the folder later; "source" writes an explicit data_source_id list.
  // `argValues` is the selected ids for the active mode.
  const [argMode, setArgMode] = useState<"folder" | "source">("source");
  const [argValues, setArgValues] = useState<string[]>([]);
  const [argSearch, setArgSearch] = useState("");
  const [groupValues, setGroupValues] = useState<string[]>([]);
  const [groupSearch, setGroupSearch] = useState("");

  const ruleSetting = editLayer === "group" ? null : row.layers[editLayer];
  const current = conditionForLayer(row, editLayer);
  const active = !conditionIsEmpty(current);
  const facets = conditionFacets(row);
  // The arg picker shows only when the row is arg-scoped AND a scope binding
  // (connection + options source) was resolved for the workspace.
  const showGroupArg = facets.arg && row.tool_key === "manage_group_overrides" && !!wsId;
  const showArg =
    facets.arg && row.tool_key !== "manage_group_overrides" && !!wsId && !!argScopeConfig;

  // Lazily fetch the scope options only while the editor is open (one cached
  // registry round-trip per edit session, not on every table render).
  const { options: scopeOptions, loading: scopeLoading } = useScopeOptions(
    wsId ?? "",
    showArg ? argScopeConfig ?? null : null,
    enabled && showArg,
  );
  const { options: groupOptions, loading: groupLoading } = useGroupScopeOptions(
    wsId ?? "",
    enabled && showGroupArg,
  );

  // Seed the form from the persisted condition each time the editor opens, so an
  // abandoned edit never leaks into the next open.
  useEffect(() => {
    if (!enabled) return;
    setHosts(current?.host_allowlist ?? []);
    setActions(current?.actions ?? []);
    setExpr(current?.expr ?? "");
    setHostDraft("");
    setHostError(null);
    setArgSearch("");
    setGroupSearch("");
    setGroupValues(
      current?.arg_allowlist?.find((a) => a.arg === GROUP_ARG)?.values ?? [],
    );
    // Seed the arg picker from the stored allowlist: a folder_id entry → folder
    // mode, otherwise the data_source_id entry → source mode.
    const folderEntry = current?.arg_allowlist?.find(
      (a) => a.arg === FOLDER_ARG && a.values.length > 0,
    );
    const sourceEntry = current?.arg_allowlist?.find(
      (a) => a.arg === DATA_SOURCE_ARG && a.values.length > 0,
    );
    if (folderEntry) {
      setArgMode("folder");
      setArgValues(folderEntry.values);
    } else {
      setArgMode("source");
      setArgValues(sourceEntry?.values ?? []);
    }
    // Open the Advanced disclosure pre-expanded only when a CEL expression is
    // already set, so an existing expression is never hidden behind a click.
    setAdvancedOpen(!!current?.expr.trim());
    // Seed once per open (the rising edge of `enabled`); an in-progress edit must
    // not be reset by re-renders while the editor stays open.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled]);

  // Switching the scope axis resets the selection — folder ids and source ids are
  // different value spaces, so a selection never carries across modes.
  function setArgModeReset(mode: "folder" | "source") {
    setArgMode(mode);
    setArgValues([]);
  }

  function toggleArgValue(id: string) {
    setArgValues((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  }

  function toggleGroupValue(id: string) {
    setGroupValues((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );
  }

  function addHost() {
    const h = normalizeHost(hostDraft);
    if (!h) return;
    if (!HOST_RULE_RE.test(h)) {
      setHostError("Enter a host like firtal.com or *.firtal.com.");
      return;
    }
    setHostError(null);
    if (!hosts.includes(h)) setHosts([...hosts, h]);
    setHostDraft("");
  }

  // Toggle a preset action chip — click to add, click again to remove. Presets
  // are the literal verb list for the tool's resource model (conditionFacets),
  // so there is no free-text entry to get wrong.
  function toggleAction(a: string) {
    setActions((prev) => (prev.includes(a) ? prev.filter((x) => x !== a) : [...prev, a]));
  }

  function save() {
    // One arg-allowlist entry per condition: the gate ANDs entries, so a folder
    // OR source rule is expressed as a single entry on the chosen axis.
    const arg_allowlist =
      showGroupArg && groupValues.length > 0
        ? [{ arg: GROUP_ARG, values: groupValues }]
        : showArg && argValues.length > 0
          ? [{ arg: argMode === "folder" ? FOLDER_ARG : DATA_SOURCE_ARG, values: argValues }]
          : [];
    const next: ToolCondition = {
      host_allowlist: hosts,
      actions,
      arg_allowlist,
      expr: expr.trim(),
    };
    onChange(conditionIsEmpty(next) ? null : next);
    onClose();
  }

  function clear() {
    onChange(null);
    onClose();
  }

  const draftEmpty = conditionIsEmpty({
    host_allowlist: hosts,
    actions,
    arg_allowlist:
      showGroupArg && groupValues.length > 0
        ? [{ arg: GROUP_ARG, values: groupValues }]
        : showArg && argValues.length > 0
          ? [{ arg: argMode === "folder" ? FOLDER_ARG : DATA_SOURCE_ARG, values: argValues }]
          : [],
    expr,
  });

  return (
        <div className="flex flex-col gap-4" data-testid={`condition-editor-${row.tool_key}`}>
          <div className="flex flex-col gap-1">
            <PopoverTitle className="text-sm font-semibold">When this rule applies</PopoverTitle>
            <PopoverDescription className="text-xs text-muted-foreground">
              Narrows when the <strong>{SETTING_LABEL[ruleSetting!]}</strong> rule applies —
              it never changes the decision. Empty means it always applies.
            </PopoverDescription>
          </div>

          {facets.host && (
            <div className="flex flex-col gap-2">
              <Label className="flex items-center gap-1.5 text-xs">
                <Globe className="size-3.5" /> Host allow-list
              </Label>
              <div className="flex gap-2">
                <Input
                  value={hostDraft}
                  onChange={(e) => {
                    setHostDraft(e.target.value);
                    setHostError(null);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      addHost();
                    }
                  }}
                  placeholder="firtal.com or *.firtal.com"
                  className="h-8"
                  aria-label="Add host"
                />
                <Button type="button" size="sm" variant="outline" onClick={addHost}>
                  <Plus className="size-4" />
                </Button>
              </div>
              {hostError && <p className="text-xs text-destructive">{hostError}</p>}
              <ConditionChips items={hosts} onRemove={(h) => setHosts(hosts.filter((x) => x !== h))} />
            </div>
          )}

          {facets.actions.length > 0 && (
            <div className="flex flex-col gap-2">
              <Label className="text-xs">Actions</Label>
              <div className="flex flex-wrap gap-1.5">
                {facets.actions.map((a) => {
                  const selected = actions.includes(a);
                  return (
                    <button
                      key={a}
                      type="button"
                      aria-pressed={selected}
                      aria-label={`Action ${a}`}
                      onClick={() => toggleAction(a)}
                      className={cn(
                        "inline-flex items-center rounded-full border px-2.5 py-1 font-mono text-xs font-medium transition-colors",
                        selected
                          ? "border-primary bg-primary/10 text-primary"
                          : "border-border bg-background text-muted-foreground hover:bg-muted",
                      )}
                    >
                      {a}
                    </button>
                  );
                })}
              </div>
              <p className="text-xs text-muted-foreground">
                Pick which actions this rule applies to. None selected means every
                action.
              </p>
            </div>
          )}

          {showArg && (
            <ArgScopePicker
              label={argScopeConfig?.label ?? "Data sources"}
              mode={argMode}
              onModeChange={setArgModeReset}
              values={argValues}
              onToggle={toggleArgValue}
              options={scopeOptions}
              loading={scopeLoading}
              search={argSearch}
              onSearch={setArgSearch}
            />
          )}

          {showGroupArg && (
            <GroupScopePicker
              values={groupValues}
              onToggle={toggleGroupValue}
              options={groupOptions}
              loading={groupLoading}
              search={groupSearch}
              onSearch={setGroupSearch}
            />
          )}

          {facets.cel && (
            <div className="flex flex-col gap-1">
              <button
                type="button"
                aria-expanded={advancedOpen}
                onClick={() => setAdvancedOpen((o) => !o)}
                className="inline-flex w-fit items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
              >
                {advancedOpen ? (
                  <ChevronDown className="size-3.5" />
                ) : (
                  <ChevronRight className="size-3.5" />
                )}
                Advanced (CEL)
              </button>
              {advancedOpen && (
                <div className="mt-1 flex flex-col gap-2">
                  <Textarea
                    id={`cel-${row.tool_key}`}
                    value={expr}
                    onChange={(e) => setExpr(e.target.value)}
                    placeholder="request.time.getHours() >= 8 && request.time.getHours() < 17"
                    className="min-h-16 font-mono text-xs"
                    aria-label="CEL expression"
                  />
                  <p className="text-xs text-muted-foreground">
                    A CEL expression for genuine dynamics (e.g. business hours). Leave
                    empty unless you need it.
                  </p>
                </div>
              )}
            </div>
          )}

          <div className="flex items-center justify-between gap-2">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="text-muted-foreground"
              onClick={clear}
              disabled={!active && draftEmpty}
            >
              Clear
            </Button>
            <div className="flex gap-2">
              <Button type="button" size="sm" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="button" size="sm" onClick={save}>
                Save
              </Button>
            </div>
          </div>
        </div>
  );
}

function GroupScopePicker({
  values,
  onToggle,
  options,
  loading,
  search,
  onSearch,
}: {
  values: string[];
  onToggle: (id: string) => void;
  options: ScopeOption[];
  loading: boolean;
  search: string;
  onSearch: (s: string) => void;
}) {
  const q = search.trim().toLowerCase();
  const rows = options.filter((o) => !q || o.name.toLowerCase().includes(q));

  return (
    <div className="flex flex-col gap-2">
      <Label className="flex items-center gap-1.5 text-xs">
        <UsersRound className="size-3.5" /> Groups
      </Label>

      <div className="relative">
        <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder="Search groups…"
          className="h-8 pl-7"
          aria-label="Search groups"
        />
      </div>

      <div className="max-h-52 overflow-y-auto rounded-md border">
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-6 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" /> Loading…
          </div>
        ) : options.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">
            No groups available.
          </p>
        ) : rows.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">
            No groups match.
          </p>
        ) : (
          <ul className="divide-y">
            {rows.map((o) => {
              const selected = values.includes(o.id);
              return (
                <li key={o.id}>
                  <button
                    type="button"
                    aria-pressed={selected}
                    onClick={() => onToggle(o.id)}
                    className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted"
                  >
                    <Checkbox checked={selected} className="pointer-events-none" />
                    <span className="flex-1 truncate">{o.name}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <p className="text-xs text-muted-foreground">
        Allows only the selected groups. None selected means every group.
      </p>
    </div>
  );
}

// ArgScopePicker is the search + multi-select for the data-source-scoping WHEN
// term (FIR-2083). A segmented control picks the axis: "Folders" writes a
// folder_id allowlist (auto-covers sources added to the folder later), "Sources"
// writes an explicit data_source_id list. The gate ANDs allowlist entries, so a
// rule scopes on exactly one axis — the segmented control keeps that invariant.
function ArgScopePicker({
  label,
  mode,
  onModeChange,
  values,
  onToggle,
  options,
  loading,
  search,
  onSearch,
}: {
  label: string;
  mode: "folder" | "source";
  onModeChange: (mode: "folder" | "source") => void;
  values: string[];
  onToggle: (id: string) => void;
  options: ScopeOption[];
  loading: boolean;
  search: string;
  onSearch: (s: string) => void;
}) {
  const groups = groupByFolder(options);
  const q = search.trim().toLowerCase();
  // Folder rows: one per real folder (a folderId), filtered by the search.
  const folderRows = groups
    .filter((g) => g.folderId)
    .filter((g) => !q || g.folder.toLowerCase().includes(q));
  // Source rows: keep the folder grouping for headings, filtered by source name,
  // tag, or folder name so a search finds a source by any of them.
  const sourceGroups = groups
    .map((g) => ({
      ...g,
      options: g.options.filter(
        (o) =>
          !q ||
          o.name.toLowerCase().includes(q) ||
          o.folder?.toLowerCase().includes(q) ||
          (o.tags ?? []).some((t) => t.toLowerCase().includes(q)),
      ),
    }))
    .filter((g) => g.options.length > 0);

  return (
    <div className="flex flex-col gap-2">
      <Label className="flex items-center gap-1.5 text-xs">
        <Database className="size-3.5" /> {label}
      </Label>

      {/* Segmented axis selector. */}
      <div className="flex w-full overflow-hidden rounded-md border text-xs">
        {(["source", "folder"] as const).map((m) => (
          <button
            key={m}
            type="button"
            aria-pressed={mode === m}
            onClick={() => onModeChange(m)}
            className={cn(
              "flex flex-1 items-center justify-center gap-1.5 px-2 py-1.5 font-medium transition-colors",
              mode === m
                ? "bg-primary/10 text-primary"
                : "bg-background text-muted-foreground hover:bg-muted",
            )}
          >
            {m === "folder" ? <Folder className="size-3.5" /> : <Database className="size-3.5" />}
            {m === "folder" ? "Folders" : "Specific sources"}
          </button>
        ))}
      </div>

      <div className="relative">
        <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder={mode === "folder" ? "Search folders…" : "Search sources…"}
          className="h-8 pl-7"
          aria-label={mode === "folder" ? "Search folders" : "Search sources"}
        />
      </div>

      <div className="max-h-52 overflow-y-auto rounded-md border">
        {loading ? (
          <div className="flex items-center justify-center gap-2 py-6 text-xs text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" /> Loading…
          </div>
        ) : options.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">
            No options available. Check the connection's scopable argument.
          </p>
        ) : mode === "folder" ? (
          folderRows.length === 0 ? (
            <p className="px-3 py-6 text-center text-xs text-muted-foreground">
              No folders match.
            </p>
          ) : (
            <ul className="divide-y">
              {folderRows.map((g) => {
                const selected = values.includes(g.folderId);
                return (
                  <li key={g.folderId}>
                    <button
                      type="button"
                      aria-pressed={selected}
                      onClick={() => onToggle(g.folderId)}
                      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted"
                    >
                      <Checkbox checked={selected} className="pointer-events-none" />
                      <Folder className="size-3.5 shrink-0 text-muted-foreground" />
                      <span className="flex-1 truncate">{g.folder}</span>
                      <span className="shrink-0 text-xs text-muted-foreground">
                        {g.options.length}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )
        ) : sourceGroups.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">
            No sources match.
          </p>
        ) : (
          <ul>
            {sourceGroups.map((g) => (
              <li key={g.folderId || g.folder}>
                <div className="sticky top-0 bg-muted/60 px-3 py-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                  {g.folder}
                </div>
                <ul className="divide-y">
                  {g.options.map((o) => {
                    const selected = values.includes(o.id);
                    return (
                      <li key={o.id}>
                        <button
                          type="button"
                          aria-pressed={selected}
                          onClick={() => onToggle(o.id)}
                          className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted"
                        >
                          <Checkbox checked={selected} className="pointer-events-none" />
                          <span className="flex-1 truncate">{o.name}</span>
                          {(o.tags ?? []).slice(0, 2).map((t) => (
                            <Badge key={t} variant="secondary" className="shrink-0 px-1.5 py-0 text-[10px]">
                              {t}
                            </Badge>
                          ))}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </li>
            ))}
          </ul>
        )}
      </div>

      <p className="text-xs text-muted-foreground">
        {mode === "folder"
          ? "Allows every source in the selected folders — including ones added later."
          : "Allows only the selected sources. None selected means every source."}
      </p>
    </div>
  );
}

// --- repo groups (FIR-2505 slice 3) -----------------------------------------

// GroupCapabilityRow is the redesigned full-width sub-row inside a collapsible
// group (a repo's read/checkout/push, a credential box's actions). It matches the
// catalog leaf-row layout exactly: the capability name takes the full width and a
// single CatalogDecisionControl pill holds BOTH the decision and the When editor
// (FIR-2706 follow-up — Jesper: "Repos and groups get the same design"). This
// replaces the old two-control bar (DecisionControl + a separate When button) that
// crowded the name on mobile and made groups read differently from the catalog.
function GroupCapabilityRow({
  row,
  editLayer,
  busy,
  testid,
  onDecision,
  onCondition,
  wsId,
  argScopeConfig,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
  busy: boolean;
  testid: string;
  onDecision: (setting: ToolSetting) => void;
  onCondition: (condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
}) {
  return (
    <div
      data-testid={testid}
      className="flex items-center justify-between gap-4 border-t px-4 py-3 first:border-t-0"
    >
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{row.title || row.tool_key}</div>
        <div className="truncate font-mono text-xs text-muted-foreground">
          {row.tool_key}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <CatalogDecisionControl
          row={row}
          editLayer={editLayer}
          disabled={busy}
          onDecision={onDecision}
          onCondition={onCondition}
          wsId={wsId}
          argScopeConfig={argScopeConfig}
        />
      </div>
    </div>
  );
}

// RepoGroupData is one repo's collapsible group: the repo URL plus its (up to
// three) capability rows in read → checkout → push order.
interface RepoGroupData {
  url: string;
  rows: ToolPolicyRow[];
}

// groupRepoRows buckets the per-repo rows by URL, orders each group's rows by
// REPO_CAP_ORDER, and narrows by the free-text search on the URL. Groups are
// sorted by URL so the list is stable.
export function groupRepoRows(rows: ToolPolicyRow[], search: string): RepoGroupData[] {
  const q = search.trim().toLowerCase();
  const byUrl = new Map<string, ToolPolicyRow[]>();
  for (const r of rows) {
    if (q && !r.resource_pattern.toLowerCase().includes(q)) continue;
    const list = byUrl.get(r.resource_pattern);
    if (list) list.push(r);
    else byUrl.set(r.resource_pattern, [r]);
  }
  const rank = (key: string) => {
    const i = REPO_CAP_ORDER.indexOf(key);
    return i === -1 ? REPO_CAP_ORDER.length : i;
  };
  return [...byUrl.entries()]
    .map(([url, rs]) => ({
      url,
      rows: [...rs].sort((a, b) => rank(a.tool_key) - rank(b.tool_key)),
    }))
    .sort((a, b) => a.url.localeCompare(b.url));
}

// repoGroupVerdict folds a group's rows into one header value: the shared
// Effective when all three agree, else "mixed". `overridden` is true when any
// capability carries an explicit setting at the page's own layer.
function repoGroupVerdict(
  group: RepoGroupData,
  editLayer: ToolLayer,
): { setting: ToolEffectiveSetting | "mixed"; overridden: boolean } {
  const settings = new Set(group.rows.map((r) => r.effective.setting));
  const overridden = group.rows.some((r) => !!r.layers[editLayer]);
  const only = settings.size === 1 ? [...settings][0] : undefined;
  return { setting: only ?? "mixed", overridden };
}

function RepoSection({
  groups,
  editLayer,
  busy,
  onSetCapability,
  onSetGroup,
  onSetCondition,
  wsId,
  argScopeConfig,
}: {
  groups: RepoGroupData[];
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (url: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: RepoGroupData, setting: ToolSetting) => void;
  /** Write/clear the When condition on one repo capability row. */
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
}) {
  return (
    <div className="flex flex-col gap-2" data-testid="repo-policy-section">
      <div className="flex items-center gap-2">
        <FolderGit2 className="size-4 text-muted-foreground" />
        <h3 className="text-base font-semibold">Repositories</h3>
        <span className="font-mono text-xs text-muted-foreground">
          {groups.length === 1 ? "1 repo" : `${groups.length} repos`}
        </span>
      </div>
      <p className="max-w-xl text-sm text-muted-foreground">
        Set a whole repository, or expand it to decide read, check out and push
        separately. Setting the repository cascades to all three.
      </p>
      <div className="flex flex-col gap-2">
        {groups.map((group) => (
          <RepoGroup
            key={group.url}
            group={group}
            editLayer={editLayer}
            busy={busy}
            onSetCapability={onSetCapability}
            onSetGroup={onSetGroup}
            onSetCondition={onSetCondition}
            wsId={wsId}
            argScopeConfig={argScopeConfig}
          />
        ))}
      </div>
    </div>
  );
}

function RepoGroup({
  group,
  editLayer,
  busy,
  onSetCapability,
  onSetGroup,
  onSetCondition,
  wsId,
  argScopeConfig,
}: {
  group: RepoGroupData;
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (url: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: RepoGroupData, setting: ToolSetting) => void;
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
}) {
  const [open, setOpen] = useState(false);
  const verdict = repoGroupVerdict(group, editLayer);
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <div
      className="overflow-hidden rounded-xl border bg-background shadow-sm"
      data-testid={`repo-group-${group.url}`}
    >
      <div className="flex items-center justify-between gap-3 bg-muted/40 px-4 py-3">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-2 text-left"
        >
          <Chevron className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate font-mono text-sm">{group.url}</span>
        </button>
        <RepoGroupControl
          verdict={verdict}
          disabled={busy}
          onChange={(s) => onSetGroup(group, s)}
        />
      </div>
      {open && (
        <div className="border-t">
          {group.rows.map((row) => (
            <GroupCapabilityRow
              key={`${row.tool_key}:${row.resource_pattern}`}
              testid={`repo-cap-${row.tool_key}-${row.resource_pattern}`}
              row={row}
              editLayer={editLayer}
              busy={busy}
              onDecision={(s) => onSetCapability(group.url, row.tool_key, s)}
              onCondition={(c) => onSetCondition(row, c)}
              wsId={wsId}
              argScopeConfig={argScopeConfig}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// RepoGroupControl is the header pill that cascades one choice to every
// capability of the repo. It shows the shared verdict, or a neutral "Mixed" when
// the three capabilities disagree.
function RepoGroupControl({
  verdict,
  disabled,
  onChange,
}: {
  verdict: { setting: ToolEffectiveSetting | "mixed"; overridden: boolean };
  disabled?: boolean;
  onChange: (setting: ToolSetting) => void;
}) {
  const concrete = verdict.setting === "mixed" ? undefined : verdict.setting;
  const Icon = concrete ? VERDICT_ICON[concrete] : FolderGit2;
  const label = concrete ? SETTING_LABEL[concrete] : "Mixed";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={`Repository decision: ${label}`}
        className={cn(
          "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold transition-colors disabled:opacity-50",
          concrete ? VERDICT_PILL[concrete] : "border-border bg-muted text-muted-foreground",
          verdict.overridden && "ring-1 ring-primary/40",
        )}
      >
        <Icon className="size-3.5" />
        {label}
        <ChevronDown className="size-3 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-40">
        {SETTING_CHOICES.map((choice) => (
          <DropdownMenuItem
            key={choice}
            onClick={() => onChange(choice)}
            className={cn("text-sm", choice === "inherit" && "text-muted-foreground")}
          >
            {SETTING_LABEL[choice]}
            <span className="ml-auto text-xs text-muted-foreground">
              {choice === "inherit" ? "clears all" : "all three"}
            </span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// CredentialGroupData is one Agent Vault box's collapsible group: its resource
// pattern (`agentvault-vault:<name>` or `cerebro-credential:<uuid>`), the display
// label (the box name), and its capability rows in CREDENTIAL_CAP_ORDER.
interface CredentialGroupData {
  resource: string;
  label: string;
  rows: ToolPolicyRow[];
}

// groupCredentialRows buckets the per-credential rows by resource pattern, labels
// each group with the box name (the rows' Category), orders each group's rows by
// CREDENTIAL_CAP_ORDER, and narrows by the free-text search on the box name.
// Groups are sorted by label so the list is stable.
export function groupCredentialRows(
  rows: ToolPolicyRow[],
  search: string,
): CredentialGroupData[] {
  const q = search.trim().toLowerCase();
  const byResource = new Map<string, ToolPolicyRow[]>();
  for (const r of rows) {
    const list = byResource.get(r.resource_pattern);
    if (list) list.push(r);
    else byResource.set(r.resource_pattern, [r]);
  }
  const rank = (key: string) => {
    const i = CREDENTIAL_CAP_ORDER.indexOf(key);
    return i === -1 ? CREDENTIAL_CAP_ORDER.length : i;
  };
  const labelFor = (resource: string, rs: ToolPolicyRow[]) =>
    rs[0]?.category?.trim() || resource.replace(/^[^:]*:/, "") || resource;
  return [...byResource.entries()]
    .map(([resource, rs]) => ({
      resource,
      label: labelFor(resource, rs),
      rows: [...rs].sort((a, b) => rank(a.tool_key) - rank(b.tool_key)),
    }))
    .filter((g) => !q || g.label.toLowerCase().includes(q))
    .sort((a, b) => a.label.localeCompare(b.label));
}

// FIR-2441: credential boxes follow the vault-naming convention
// "<track>-<owner>-<credential>" (e.g. agents-mia-agent-vault,
// members-jesper-bigquery), so the flat box list nests into a tree
// track › owner › credential. Setting a decision on a track or owner node
// cascades to every credential box beneath it — the naming IS the permission
// structure. Boxes whose name does not fit the convention (e.g. "default", or a
// registry credential UUID) stay at the top level as ungrouped leaves.
const CREDENTIAL_TRACKS = ["agents", "members", "shared"];

// A node in the credential permission tree. A branch (track/owner) carries
// `children` and no `group`; a leaf carries `group` (one Agent Vault box) and no
// children. `key` is stable across renders; `label` is the segment shown.
export interface CredentialTreeNode {
  key: string;
  label: string;
  children: CredentialTreeNode[];
  group?: CredentialGroupData;
}

// buildCredentialTree nests the flat credential groups by parsing each box name
// on "-": first segment = track (agents/members/shared), second = owner, the
// rest = the credential label. Every level is sorted alphabetically so the tree
// is stable. Names without a known track prefix are top-level leaves.
export function buildCredentialTree(
  groups: CredentialGroupData[],
): CredentialTreeNode[] {
  const roots: CredentialTreeNode[] = [];
  const findOrAdd = (
    siblings: CredentialTreeNode[],
    key: string,
    label: string,
  ): CredentialTreeNode => {
    let node = siblings.find((s) => s.key === key);
    if (!node) {
      node = { key, label, children: [] };
      siblings.push(node);
    }
    return node;
  };
  for (const group of groups) {
    const name = group.label;
    const firstDash = name.indexOf("-");
    const track = firstDash === -1 ? "" : name.slice(0, firstDash);
    if (firstDash === -1 || !CREDENTIAL_TRACKS.includes(track)) {
      roots.push({ key: group.resource, label: name, children: [], group });
      continue;
    }
    const rest = name.slice(firstDash + 1); // e.g. "mia-agent-vault"
    const secondDash = rest.indexOf("-");
    const owner = secondDash === -1 ? rest : rest.slice(0, secondDash);
    const leafLabel = secondDash === -1 ? rest : rest.slice(secondDash + 1);
    const trackNode = findOrAdd(roots, `track:${track}`, track);
    const ownerNode = findOrAdd(
      trackNode.children,
      `track:${track}/owner:${owner}`,
      owner,
    );
    ownerNode.children.push({
      key: group.resource,
      label: leafLabel || name,
      children: [],
      group,
    });
  }
  const sortRec = (nodes: CredentialTreeNode[]) => {
    nodes.sort((a, b) => a.label.localeCompare(b.label));
    for (const n of nodes) sortRec(n.children);
  };
  sortRec(roots);
  return roots;
}

// subtreeGroups collects every leaf credential box under a node (the node itself
// when it is a leaf) — the exact set a branch-level cascade writes to.
export function subtreeGroups(node: CredentialTreeNode): CredentialGroupData[] {
  return node.group ? [node.group] : node.children.flatMap(subtreeGroups);
}

// credentialNodeVerdict folds every capability row beneath a branch into one
// header value: the shared Effective when all agree, else "mixed". `overridden`
// is true when any row beneath carries an explicit setting at the page's layer.
function credentialNodeVerdict(
  node: CredentialTreeNode,
  editLayer: ToolLayer,
): { setting: ToolEffectiveSetting | "mixed"; overridden: boolean } {
  const rows = subtreeGroups(node).flatMap((g) => g.rows);
  const settings = new Set(rows.map((r) => r.effective.setting));
  const overridden = rows.some((r) => !!r.layers[editLayer]);
  const only = settings.size === 1 ? [...settings][0] : undefined;
  return { setting: only ?? "mixed", overridden };
}

// credentialGroupVerdict folds a box's rows into one header value: the shared
// Effective when all agree, else "mixed". `overridden` is true when any
// capability carries an explicit setting at the page's own layer.
function credentialGroupVerdict(
  group: CredentialGroupData,
  editLayer: ToolLayer,
): { setting: ToolEffectiveSetting | "mixed"; overridden: boolean } {
  const settings = new Set(group.rows.map((r) => r.effective.setting));
  const overridden = group.rows.some((r) => !!r.layers[editLayer]);
  const only = settings.size === 1 ? [...settings][0] : undefined;
  return { setting: only ?? "mixed", overridden };
}

function CredentialSection({
  groups,
  editLayer,
  busy,
  onSetCapability,
  onSetGroup,
  onSetCondition,
  wsId,
  argScopeConfig,
}: {
  groups: CredentialGroupData[];
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (resource: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: CredentialGroupData, setting: ToolSetting) => void;
  /** Write/clear the When condition on one credential capability row. */
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
}) {
  return (
    <div className="flex flex-col gap-2" data-testid="credential-policy-section">
      <div className="flex items-center gap-2">
        <KeyRound className="size-4 text-muted-foreground" />
        <h3 className="text-base font-semibold">Credentials</h3>
        <span className="font-mono text-xs text-muted-foreground">
          {groups.length === 1 ? "1 credential" : `${groups.length} credentials`}
        </span>
      </div>
      <p className="max-w-xl text-sm text-muted-foreground">
        Grouped by name (track › owner › credential). Set a whole track, a whole
        owner, or one credential — the choice cascades to everything beneath it.
        Expand a credential to decide each action separately.
      </p>
      <div className="flex flex-col gap-2">
        {buildCredentialTree(groups).map((node) => (
          <CredentialTreeNodeView
            key={node.key}
            node={node}
            depth={0}
            editLayer={editLayer}
            busy={busy}
            onSetCapability={onSetCapability}
            onSetGroup={onSetGroup}
            onSetCondition={onSetCondition}
            wsId={wsId}
            argScopeConfig={argScopeConfig}
          />
        ))}
      </div>
    </div>
  );
}

// CredentialTreeNodeView renders one node of the credential permission tree: a
// leaf delegates to CredentialGroup (the box's per-action fold-out); a branch
// (track/owner) renders a collapsible header whose Decision control cascades to
// every credential box beneath it, then its children recursively. `depth` only
// drives the left indent so the nesting reads visually.
function CredentialTreeNodeView({
  node,
  depth,
  editLayer,
  busy,
  onSetCapability,
  onSetGroup,
  onSetCondition,
  wsId,
  argScopeConfig,
}: {
  node: CredentialTreeNode;
  depth: number;
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (resource: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: CredentialGroupData, setting: ToolSetting) => void;
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
}) {
  const [open, setOpen] = useState(true);
  if (node.group) {
    return (
      <CredentialGroup
        group={node.group}
        editLayer={editLayer}
        busy={busy}
        onSetCapability={onSetCapability}
        onSetGroup={onSetGroup}
        onSetCondition={onSetCondition}
        wsId={wsId}
        argScopeConfig={argScopeConfig}
      />
    );
  }
  const verdict = credentialNodeVerdict(node, editLayer);
  const Chevron = open ? ChevronDown : ChevronRight;
  const leaves = subtreeGroups(node);
  return (
    <div
      className="overflow-hidden rounded-xl border bg-background shadow-sm"
      data-testid={`credential-branch-${node.key}`}
    >
      <div className="flex items-center justify-between gap-3 bg-muted/40 px-4 py-3">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-2 text-left"
        >
          <Chevron className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate text-sm font-semibold">{node.label}</span>
          <span className="font-mono text-xs text-muted-foreground">
            {leaves.length === 1 ? "1 credential" : `${leaves.length} credentials`}
          </span>
        </button>
        <CredentialGroupControl
          verdict={verdict}
          disabled={busy}
          onChange={(s) => leaves.forEach((g) => onSetGroup(g, s))}
        />
      </div>
      {open && (
        <div className="flex flex-col gap-2 border-t p-2 pl-4">
          {node.children.map((child) => (
            <CredentialTreeNodeView
              key={child.key}
              node={child}
              depth={depth + 1}
              editLayer={editLayer}
              busy={busy}
              onSetCapability={onSetCapability}
              onSetGroup={onSetGroup}
              onSetCondition={onSetCondition}
              wsId={wsId}
              argScopeConfig={argScopeConfig}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function CredentialGroup({
  group,
  editLayer,
  busy,
  onSetCapability,
  onSetGroup,
  onSetCondition,
  wsId,
  argScopeConfig,
}: {
  group: CredentialGroupData;
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (resource: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: CredentialGroupData, setting: ToolSetting) => void;
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
  wsId?: string;
  argScopeConfig?: ScopeConfig | null;
}) {
  const [open, setOpen] = useState(false);
  const verdict = credentialGroupVerdict(group, editLayer);
  const Chevron = open ? ChevronDown : ChevronRight;

  // A box with a single action (Agent Vault boxes expose only "Use secret") IS
  // one permission row — render it flat with one decision, no pointless fold-out.
  if (group.rows.length === 1) {
    const row = group.rows[0]!;
    return (
      <div
        className="flex items-center justify-between gap-4 rounded-xl border bg-background px-4 py-3 shadow-sm"
        data-testid={`credential-group-${group.resource}`}
      >
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{group.label}</span>
        <div className="flex shrink-0 items-center gap-2">
          <CatalogDecisionControl
            row={row}
            editLayer={editLayer}
            disabled={busy}
            onDecision={(s) => onSetCapability(group.resource, row.tool_key, s)}
            onCondition={(c) => onSetCondition(row, c)}
            wsId={wsId}
            argScopeConfig={argScopeConfig}
          />
        </div>
      </div>
    );
  }

  return (
    <div
      className="overflow-hidden rounded-xl border bg-background shadow-sm"
      data-testid={`credential-group-${group.resource}`}
    >
      <div className="flex items-center justify-between gap-3 bg-muted/40 px-4 py-3">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="flex min-w-0 items-center gap-2 text-left"
        >
          <Chevron className="size-4 shrink-0 text-muted-foreground" />
          <span className="truncate text-sm font-medium">{group.label}</span>
        </button>
        <CredentialGroupControl
          verdict={verdict}
          disabled={busy}
          onChange={(s) => onSetGroup(group, s)}
        />
      </div>
      {open && (
        <div className="border-t">
          {group.rows.map((row) => (
            <GroupCapabilityRow
              key={`${row.tool_key}:${row.resource_pattern}`}
              testid={`credential-cap-${row.tool_key}-${row.resource_pattern}`}
              row={row}
              editLayer={editLayer}
              busy={busy}
              onDecision={(s) => onSetCapability(group.resource, row.tool_key, s)}
              onCondition={(c) => onSetCondition(row, c)}
              wsId={wsId}
              argScopeConfig={argScopeConfig}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// CredentialGroupControl is the header pill that cascades one choice to every
// capability of a credential box. It shows the shared verdict, or a neutral
// "Mixed" when the box's capabilities disagree.
function CredentialGroupControl({
  verdict,
  disabled,
  onChange,
}: {
  verdict: { setting: ToolEffectiveSetting | "mixed"; overridden: boolean };
  disabled?: boolean;
  onChange: (setting: ToolSetting) => void;
}) {
  const concrete = verdict.setting === "mixed" ? undefined : verdict.setting;
  const Icon = concrete ? VERDICT_ICON[concrete] : KeyRound;
  const label = concrete ? SETTING_LABEL[concrete] : "Mixed";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={`Credential decision: ${label}`}
        className={cn(
          "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold transition-colors disabled:opacity-50",
          concrete ? VERDICT_PILL[concrete] : "border-border bg-muted text-muted-foreground",
          verdict.overridden && "ring-1 ring-primary/40",
        )}
      >
        <Icon className="size-3.5" />
        {label}
        <ChevronDown className="size-3 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-40">
        {SETTING_CHOICES.map((choice) => (
          <DropdownMenuItem
            key={choice}
            onClick={() => onChange(choice)}
            className={cn("text-sm", choice === "inherit" && "text-muted-foreground")}
          >
            {SETTING_LABEL[choice]}
            <span className="ml-auto text-xs text-muted-foreground">
              {choice === "inherit" ? "clears all" : "all actions"}
            </span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
