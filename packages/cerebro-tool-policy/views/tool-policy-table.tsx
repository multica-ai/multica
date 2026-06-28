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

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronRight,
  Database,
  Folder,
  FolderGit2,
  Globe,
  KeyRound,
  Loader2,
  Lock,
  Plus,
  Search,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
  SlidersHorizontal,
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@multica/ui/components/ui/select";
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
  classFacets,
  classifySideEffect,
  conditionFacets,
  isLockedFromElsewhere,
  SIDE_EFFECT_LABEL,
  SIDE_EFFECTS,
  toolPolicyTableOptions,
  useClearToolPolicy,
  useSetToolPolicy,
  type ClassFacet,
  type SideEffect,
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
import { FirtalRegistryRowConfigure } from "./firtal-registry-row-configure";
import { ConnectionRowConfigure } from "./connection-row-configure";
import { FirtalRegistryDataSourceConfigure } from "./firtal-registry-data-source-sheet";

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

/** Restricts the rows shown to a specific category of tools. */
export type ToolPolicyTabFilter =
  | "repos"
  | "connections"
  | "runtime"
  | "multica"
  | "credentials";

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
   * When set, restricts which rows are visible:
   *   "repos"       — only per-repo rows (read/checkout/push per repository)
   *   "connections" — only workspace connection tools (source === "connection")
   *   "credentials" — only per-credential rows (source === "credential")
   *   "runtime"     — only runtime/daemon capability tools
   *   "multica"     — everything else (issues, agents, comments, etc.)
   * When omitted, all rows are shown (original behaviour).
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
const SETTING_LABEL: Record<ToolSetting, string> = {
  allow: "Allow",
  ask: "Ask",
  deny: "Deny",
  inherit: "Inherit",
};
const DECISION_FILTERS: ToolEffectiveSetting[] = ["allow", "ask", "deny"];

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
  const [classes, setClasses] = useState<Set<string>>(new Set());
  const [effects, setEffects] = useState<Set<SideEffect>>(new Set());
  const [decisions, setDecisions] = useState<Set<ToolEffectiveSetting>>(new Set());
  const [showInherited, setShowInherited] = useState(true);

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

  // tabFilter narrows which rows this instance shows. TanStack Query deduplicates
  // the underlying fetch, so multiple ToolPolicyTable instances on the same page
  // (e.g. tabbed workspace permissions) share a single network request.
  const capRows = useMemo(() => {
    // A runtime-reported tool is one a runtime actually advertised — built-ins
    // and MCP actions land with source "runtime_report" (the snapshot) or "scan"
    // (the daemon tools/list probe). The platform "Runtimes" category is a
    // different thing: the runtime *admin* actions (manage_runtime, create_runtime)
    // from platformcatalog. The Runtime tab must show the reported tools (FIR-1708
    // D(a)) — filtering on category === "Runtimes" hid them in the Multica tab.
    const isRuntimeReported = (r: ToolPolicyRow) =>
      r.source === "runtime_report" || r.source === "scan";
    if (tabFilter === "repos") return [];
    if (!tabFilter) return allCapRows;
    if (tabFilter === "connections") return allCapRows.filter((r) => r.source === "connection");
    // Credential rows carry a resource pattern (so they live in `rows`, not
    // `allCapRows`), one row per box per capability. Like repos they render as
    // collapsible per-box groups (credentialGroups below), NOT as flat catalog
    // rows — so the flat table excludes them entirely on the credentials tab.
    if (tabFilter === "credentials") return [];
    if (tabFilter === "runtime")
      return allCapRows.filter((r) => isRuntimeReported(r) || r.category === "Runtimes");
    // "multica" = everything that isn't a connection, a runtime-reported tool, or
    // a runtime-admin action (those have their own tabs above). Credential rows are
    // already excluded here because they carry a resource pattern (not in allCapRows).
    return allCapRows.filter(
      (r) =>
        r.source !== "connection" &&
        !isRuntimeReported(r) &&
        r.category !== "Runtimes",
    );
  }, [tabFilter, allCapRows, allCredentialRows]);

  const repoRows = useMemo(() => {
    if (!tabFilter || tabFilter === "repos") return allRepoRows;
    return [];
  }, [tabFilter, allRepoRows]);

  const facets = useMemo(() => classFacets(capRows), [capRows]);
  const filtered = useMemo(
    () => filterRows(capRows, { search, classes, effects, decisions, showInherited, editLayer }),
    [capRows, search, classes, effects, decisions, showInherited, editLayer],
  );
  // Repo groups are keyed by URL and narrowed only by the free-text search — the
  // class/side-effect/decision facets describe capabilities, not repos.
  const repoGroups = useMemo(() => groupRepoRows(repoRows, search), [repoRows, search]);
  // Credential groups: one collapsible group per Agent Vault box, shown only on
  // the credentials tab. Like repo groups they are keyed by resource_pattern and
  // narrowed only by the free-text search (on the box name).
  const credentialGroups = useMemo(
    () =>
      tabFilter === "credentials" ? groupCredentialRows(allCredentialRows, search) : [],
    [tabFilter, allCredentialRows, search],
  );

  const busy = setPolicy.isPending || clearPolicy.isPending;

  function applySetting(toolKey: string, setting: ToolSetting, resourcePattern?: string) {
    const scope = resourcePattern ? { resource_pattern: resourcePattern } : {};
    if (setting === "inherit") {
      clearPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId, ...scope });
      return;
    }
    setPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId, setting, ...scope });
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
    for (const row of filtered) {
      // TECH-3287 hul 6: "Allow all" can't loosen a row a higher layer blocks —
      // skip those instead of firing a silent dead write that reverts on refetch.
      if (setting === "allow" && isLockedFromElsewhere(row, editLayer)) continue;
      setPolicy.mutate({ tool_key: row.tool_key, layer: editLayer, subject_id: subjectId, setting });
    }
  }

  return (
    <div className="flex flex-col gap-4" data-testid="tool-policy-table">
      <CatalogHeader
        shown={filtered.length}
        total={capRows.length + repoRows.length}
        busy={busy}
        onBulk={bulkSet}
      />

      <FilterBar
        search={search}
        onSearch={setSearch}
        facets={facets}
        classes={classes}
        onToggleClass={(c) => setClasses((s) => toggle(s, c))}
        effects={effects}
        onToggleEffect={(e) => setEffects((s) => toggle(s, e))}
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
          {tabFilter === "credentials" && credentialGroups.length === 0
            ? "No credentials available yet. Connect an Agent Vault to this workspace to grant credential access here."
            : allCapRows.length === 0 && allRepoRows.length === 0
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
            />
          )}

          {filtered.length > 0 && (
          <>
          {/* Desktop: the full sortable catalog table. */}
          <div className="hidden overflow-hidden rounded-lg border md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[38%]">Tool</TableHead>
                  <TableHead>Class</TableHead>
                  <TableHead>Side effect</TableHead>
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
                        <span className="font-medium">{row.title || row.tool_key}</span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {row.tool_key}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <Badge variant="outline" className="font-normal">
                          {row.category || "Uncategorised"}
                        </Badge>
                        {row.managed_externally && <ManagedExternallyTag />}
                      </div>
                    </TableCell>
                    <TableCell>
                      <SideEffectTag effect={classifySideEffect(row)} />
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
                    <div className="truncate text-sm font-medium">
                      {row.title || row.tool_key}
                    </div>
                    <div className="truncate font-mono text-xs text-muted-foreground">
                      {row.tool_key}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
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
                  <Badge variant="outline" className="font-normal">
                    {row.category || "Uncategorised"}
                  </Badge>
                  {row.managed_externally && <ManagedExternallyTag />}
                  <SideEffectTag effect={classifySideEffect(row)} />
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
  // Credentials are a permission type only once the workspace turns the feature on
  // (FIR-1479). Gate the tab on the same flag the backend gates the rows with, so
  // the tab is a consistent permission surface the moment an admin enables it.
  const showCredentials = useFeatureFlag("cerebro_credentials_per_actor");
  return (
    // TECH-3156 Mangel 3: force the tab row horizontal. The shared Tabs primitive
    // renders its list vertically by default, so — like cost-optimization-tabs —
    // we override the list to !flex-row and each trigger to !w-auto so the tabs sit
    // on one horizontal row instead of stacked. On narrow screens the row scrolls
    // horizontally (flex-nowrap + overflow-x-auto) instead of wrapping and breaking
    // over the content below it.
    <Tabs defaultValue="multica" orientation="horizontal">
      <TabsList className="no-scrollbar !h-auto w-full max-w-full !flex-row flex-nowrap justify-start gap-1 overflow-x-auto">
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="multica">
          Multica
        </TabsTrigger>
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="runtime">
          Runtime
        </TabsTrigger>
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="repos">
          Repos
        </TabsTrigger>
        <TabsTrigger className="!w-auto !flex-none !justify-center" value="connections">
          Connections
        </TabsTrigger>
        {showCredentials && (
          <TabsTrigger className="!w-auto !flex-none !justify-center" value="credentials">
            Credentials
          </TabsTrigger>
        )}
      </TabsList>
      <TabsContent value="multica" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="multica" />
      </TabsContent>
      <TabsContent value="runtime" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="runtime" />
      </TabsContent>
      <TabsContent value="repos" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="repos" />
      </TabsContent>
      <TabsContent value="connections" className="mt-4">
        <ToolPolicyTable {...props} tabFilter="connections" />
      </TabsContent>
      {showCredentials && (
        <TabsContent value="credentials" className="mt-4">
          <ToolPolicyTable {...props} tabFilter="credentials" />
        </TabsContent>
      )}
    </Tabs>
  );
}

// --- filtering --------------------------------------------------------------

interface FilterState {
  search: string;
  classes: Set<string>;
  effects: Set<SideEffect>;
  decisions: Set<ToolEffectiveSetting>;
  showInherited: boolean;
  editLayer: ToolLayer;
}

// filterRows applies the combinable filters. Each facet (class / side effect /
// decision) is OR within itself and AND across facets; an empty set means "all".
// "Show inherited" off keeps only rows this page has explicitly authored at its
// own layer, so a reviewer can see just the overrides they own.
export function filterRows(rows: ToolPolicyRow[], f: FilterState): ToolPolicyRow[] {
  const q = f.search.trim().toLowerCase();
  return rows.filter((r) => {
    if (q && !`${r.title} ${r.tool_key} ${r.category}`.toLowerCase().includes(q)) {
      return false;
    }
    if (f.classes.size && !f.classes.has(r.category || "Uncategorised")) return false;
    if (f.effects.size && !f.effects.has(classifySideEffect(r))) return false;
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
            Permissions attach at the tool level; filter by capability class, side
            effect or decision.
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
  facets,
  classes,
  onToggleClass,
  effects,
  onToggleEffect,
  decisions,
  onToggleDecision,
  showInherited,
  onShowInherited,
  editLayerLabel,
}: {
  search: string;
  onSearch: (v: string) => void;
  facets: ClassFacet[];
  classes: Set<string>;
  onToggleClass: (c: string) => void;
  effects: Set<SideEffect>;
  onToggleEffect: (e: SideEffect) => void;
  decisions: Set<ToolEffectiveSetting>;
  onToggleDecision: (d: ToolEffectiveSetting) => void;
  showInherited: boolean;
  onShowInherited: (v: boolean) => void;
  editLayerLabel: string;
}) {
  const selectedClass = Array.from(classes)[0] ?? "__all__";
  const selectedClassLabel =
    selectedClass === "__all__" ? "All classes" : selectedClass;

  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <div className="relative w-full sm:max-w-xs">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder="Filter tools by name or class…"
            className="h-9 pl-9"
            aria-label="Filter tools"
          />
        </div>

        <Select
          value={selectedClass}
          onValueChange={(value) => {
            if (!value) return;
            if (value === "__all__") {
              if (classes.size > 0) onToggleClass(selectedClass);
              return;
            }
            if (classes.has(value)) return;
            if (selectedClass !== "__all__") onToggleClass(selectedClass);
            onToggleClass(value);
          }}
        >
          <SelectTrigger className="h-9 w-full sm:w-56" aria-label="Filter by class">
            <span data-slot="select-value" className="flex flex-1 text-left">
              {selectedClassLabel}
            </span>
          </SelectTrigger>
          <SelectContent align="start">
            <SelectItem value="__all__">All classes</SelectItem>
            {facets.map((facet) => (
              <SelectItem key={facet.category} value={facet.category}>
                {facet.category} ({facet.count})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <FilterGroup label="Side effect">
        {SIDE_EFFECTS.map((effect) => (
          <FilterChip
            key={effect}
            active={effects.has(effect)}
            onClick={() => onToggleEffect(effect)}
          >
            {SIDE_EFFECT_LABEL[effect]}
          </FilterChip>
        ))}
      </FilterGroup>

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

function SideEffectTag({ effect }: { effect: SideEffect }) {
  return (
    <Badge variant="secondary" className="font-normal">
      {SIDE_EFFECT_LABEL[effect]}
    </Badge>
  );
}

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
        {SETTING_CHOICES.map((choice) => (
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

  const ruleSetting = editLayer === "group" ? null : row.layers[editLayer];
  const hasConcreteRule =
    ruleSetting === "allow" || ruleSetting === "ask" || ruleSetting === "deny";
  const current = conditionForLayer(row, editLayer);
  const active = !conditionIsEmpty(current);

  // The contextual facets: which structured sections are worth showing for this
  // tool. `meaningful` is the gate for whether the control appears at all.
  const facets = conditionFacets(row);
  // The arg picker shows only when the row is arg-scoped AND a scope binding
  // (connection + options source) was resolved for the workspace.
  const showArg = facets.arg && !!wsId && !!argScopeConfig;
  const meaningful =
    facets.host || facets.actions.length > 0 || facets.cel || showArg;

  // Lazily fetch the scope options only while the popover is open (one cached
  // registry round-trip per edit session, not on every table render).
  const { options: scopeOptions, loading: scopeLoading } = useScopeOptions(
    wsId ?? "",
    showArg ? argScopeConfig ?? null : null,
    open && showArg,
  );

  // Group has no single condition — show nothing there.
  if (editLayer === "group") return null;

  // Hide the control entirely on a tool where a condition makes no sense AND none
  // is already set — no stray "When" affordance on a notification tool. A row
  // that already carries a condition keeps its control so the value stays
  // editable even if the heuristic would otherwise hide it.
  if (!meaningful && !active) return null;

  // Seed the form from the persisted condition each time the popover opens, so an
  // abandoned edit never leaks into the next open.
  function handleOpenChange(next: boolean) {
    if (next) {
      setHosts(current?.host_allowlist ?? []);
      setActions(current?.actions ?? []);
      setExpr(current?.expr ?? "");
      setHostDraft("");
      setHostError(null);
      setArgSearch("");
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
    }
    setOpen(next);
  }

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
      showArg && argValues.length > 0
        ? [{ arg: argMode === "folder" ? FOLDER_ARG : DATA_SOURCE_ARG, values: argValues }]
        : [];
    const next: ToolCondition = {
      host_allowlist: hosts,
      actions,
      arg_allowlist,
      expr: expr.trim(),
    };
    onChange(conditionIsEmpty(next) ? null : next);
    setOpen(false);
  }

  function clear() {
    onChange(null);
    setOpen(false);
  }

  // No concrete rule on this layer → nothing to refine. Disabled hint that names
  // the prerequisite, rather than silently creating an override. Only shown when
  // a facet IS meaningful — on a non-meaningful row we hid the control above.
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

  const draftEmpty = conditionIsEmpty({
    host_allowlist: hosts,
    actions,
    arg_allowlist:
      showArg && argValues.length > 0
        ? [{ arg: argMode === "folder" ? FOLDER_ARG : DATA_SOURCE_ARG, values: argValues }]
        : [],
    expr,
  });

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
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
              <Button type="button" size="sm" variant="outline" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="button" size="sm" onClick={save}>
                Save
              </Button>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
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
}: {
  groups: RepoGroupData[];
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (url: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: RepoGroupData, setting: ToolSetting) => void;
  /** Write/clear the When condition on one repo capability row. */
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
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
}: {
  group: RepoGroupData;
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (url: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: RepoGroupData, setting: ToolSetting) => void;
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const verdict = repoGroupVerdict(group, editLayer);
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <div className="rounded-lg border" data-testid={`repo-group-${group.url}`}>
      <div className="flex items-center justify-between gap-3 p-3">
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
            <div
              key={`${row.tool_key}:${row.resource_pattern}`}
              data-testid={`repo-cap-${row.tool_key}-${row.resource_pattern}`}
              className="flex items-center justify-between gap-3 py-2 pl-9 pr-3"
            >
              <div className="flex min-w-0 flex-col">
                <span className="text-sm">{row.title || row.tool_key}</span>
                <span className="font-mono text-xs text-muted-foreground">{row.tool_key}</span>
              </div>
              <div className="flex items-center gap-2">
                <OriginTag row={row} editLayer={editLayer} />
                <DecisionControl
                  row={row}
                  editLayer={editLayer}
                  disabled={busy}
                  onChange={(s) => onSetCapability(group.url, row.tool_key, s)}
                />
                <ConditionControl
                  row={row}
                  editLayer={editLayer}
                  disabled={busy}
                  onChange={(c) => onSetCondition(row, c)}
                />
              </div>
            </div>
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
}: {
  groups: CredentialGroupData[];
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (resource: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: CredentialGroupData, setting: ToolSetting) => void;
  /** Write/clear the When condition on one credential capability row. */
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
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
        Set a whole credential, or expand it to decide each action separately.
        Setting the credential cascades to every action.
      </p>
      <div className="flex flex-col gap-2">
        {groups.map((group) => (
          <CredentialGroup
            key={group.resource}
            group={group}
            editLayer={editLayer}
            busy={busy}
            onSetCapability={onSetCapability}
            onSetGroup={onSetGroup}
            onSetCondition={onSetCondition}
          />
        ))}
      </div>
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
}: {
  group: CredentialGroupData;
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (resource: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: CredentialGroupData, setting: ToolSetting) => void;
  onSetCondition: (row: ToolPolicyRow, condition: ToolCondition | null) => void;
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
        className="flex items-center justify-between gap-3 rounded-lg border p-3"
        data-testid={`credential-group-${group.resource}`}
      >
        <span className="truncate text-sm font-medium">{group.label}</span>
        <div className="flex items-center gap-2">
          <OriginTag row={row} editLayer={editLayer} />
          <DecisionControl
            row={row}
            editLayer={editLayer}
            disabled={busy}
            onChange={(s) => onSetCapability(group.resource, row.tool_key, s)}
          />
          <ConditionControl
            row={row}
            editLayer={editLayer}
            disabled={busy}
            onChange={(c) => onSetCondition(row, c)}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-lg border" data-testid={`credential-group-${group.resource}`}>
      <div className="flex items-center justify-between gap-3 p-3">
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
            <div
              key={`${row.tool_key}:${row.resource_pattern}`}
              data-testid={`credential-cap-${row.tool_key}-${row.resource_pattern}`}
              className="flex items-center justify-between gap-3 py-2 pl-9 pr-3"
            >
              <div className="flex min-w-0 flex-col">
                <span className="text-sm">{row.title || row.tool_key}</span>
                <span className="font-mono text-xs text-muted-foreground">{row.tool_key}</span>
              </div>
              <div className="flex items-center gap-2">
                <OriginTag row={row} editLayer={editLayer} />
                <DecisionControl
                  row={row}
                  editLayer={editLayer}
                  disabled={busy}
                  onChange={(s) => onSetCapability(group.resource, row.tool_key, s)}
                />
                <ConditionControl
                  row={row}
                  editLayer={editLayer}
                  disabled={busy}
                  onChange={(c) => onSetCondition(row, c)}
                />
              </div>
            </div>
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
