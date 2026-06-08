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
  FolderGit2,
  Lock,
  Search,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
} from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
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
import {
  classFacets,
  classifySideEffect,
  SIDE_EFFECT_LABEL,
  SIDE_EFFECTS,
  toolPolicyTableOptions,
  useClearToolPolicy,
  useSetToolPolicy,
  type ClassFacet,
  type SideEffect,
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolPolicyRow,
  type ToolSetting,
} from "../core";
import { FirtalRegistryRowConfigure } from "./firtal-registry-row-configure";

/**
 * The five surfaces (FIR-2284 Bid 5). Each page renders the same catalog but
 * authors a different rung of the Workspace › Runtime › Agent › Group › User
 * chain. `view` decides the editable layer; `subjectId` is the id that layer's
 * rows key on for this page.
 */
export type ToolPolicyView =
  | "workspace"
  | "runtime"
  | "agent"
  | "group"
  | "member";

/** Restricts the rows shown to a specific category of tools. */
export type ToolPolicyTabFilter = "repos" | "connections" | "runtime" | "multica";

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
   *   "runtime"     — only runtime/daemon capability tools
   *   "multica"     — everything else (issues, agents, comments, etc.)
   * When omitted, all rows are shown (original behaviour).
   */
  tabFilter?: ToolPolicyTabFilter;
}

export type ToolPolicyTabsProps = Omit<ToolPolicyTableProps, "tabFilter">;

// VIEW_EDIT_LAYER maps each surface to the chain layer it authors. The member
// page edits the User (ceiling) layer — "member" is the page, "user" is the rung.
const VIEW_EDIT_LAYER: Record<ToolPolicyView, ToolLayer> = {
  workspace: "workspace",
  runtime: "runtime",
  agent: "agent",
  group: "group",
  member: "user",
};

// Repo rows carry a non-empty resource_pattern (the repo URL) and render as
// collapsible groups, not as flat rows in the tool catalog (FIR-2505 slice 3).
// The order the three repo capabilities render inside a group, regardless of the
// order the server emitted them.
const REPO_CAP_ORDER = ["repo.read", "repo.checkout", "repo.push"];

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

  const rows = query.data ?? [];
  // Capability-wide rows (the flat catalog) vs. per-repo rows (collapsible
  // groups). A repo row is any row scoped to a resource (non-empty pattern).
  const allCapRows = useMemo(() => rows.filter((r) => !r.resource_pattern), [rows]);
  const allRepoRows = useMemo(() => rows.filter((r) => r.resource_pattern), [rows]);

  // tabFilter narrows which rows this instance shows. TanStack Query deduplicates
  // the underlying fetch, so multiple ToolPolicyTable instances on the same page
  // (e.g. tabbed workspace permissions) share a single network request.
  const capRows = useMemo(() => {
    if (tabFilter === "repos") return [];
    if (!tabFilter) return allCapRows;
    if (tabFilter === "connections") return allCapRows.filter((r) => r.source === "connection");
    if (tabFilter === "runtime") return allCapRows.filter((r) => r.category === "Runtimes");
    // "multica" = everything that isn't a connection or runtime tool
    return allCapRows.filter((r) => r.source !== "connection" && r.category !== "Runtimes");
  }, [tabFilter, allCapRows]);

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

  const busy = setPolicy.isPending || clearPolicy.isPending;

  function applySetting(toolKey: string, setting: ToolSetting, resourcePattern?: string) {
    const scope = resourcePattern ? { resource_pattern: resourcePattern } : {};
    if (setting === "inherit") {
      clearPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId, ...scope });
      return;
    }
    setPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId, setting, ...scope });
  }

  // Cascade one choice onto every capability of a repo (the group header
  // control): "set the repo to Allow and every row under it follows."
  function applyRepoGroup(group: RepoGroupData, setting: ToolSetting) {
    for (const row of group.rows) {
      applySetting(row.tool_key, setting, group.url);
    }
  }

  function bulkSet(setting: Exclude<ToolSetting, "inherit">) {
    for (const row of filtered) {
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
      ) : repoGroups.length === 0 && filtered.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {allCapRows.length === 0 && allRepoRows.length === 0
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
                  <TableRow key={row.tool_key} data-testid={`tool-row-${row.tool_key}`}>
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
                          onChange={(s) => applySetting(row.tool_key, s)}
                        />
                        {view === "agent" && subjectId ? (
                          <FirtalRegistryRowConfigure
                            toolKey={row.tool_key}
                            agentId={subjectId}
                            variant="outline"
                          />
                        ) : null}
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
                key={row.tool_key}
                data-testid={`tool-card-${row.tool_key}`}
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
                    <DecisionControl
                      row={row}
                      editLayer={editLayer}
                      disabled={busy}
                      onChange={(s) => applySetting(row.tool_key, s)}
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
  return (
    <Tabs defaultValue="multica" className="flex-col">
      <TabsList>
        <TabsTrigger value="multica">Multica</TabsTrigger>
        <TabsTrigger value="runtime">Runtime</TabsTrigger>
        <TabsTrigger value="repos">Repos</TabsTrigger>
        <TabsTrigger value="connections">Connections</TabsTrigger>
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
  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-3">
      <div className="relative w-full max-w-xs">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder="Filter tools by name or class…"
          className="h-9 pl-9"
          aria-label="Filter tools"
        />
      </div>

      <FilterGroup label="Class">
        {facets.map((facet) => (
          <FilterChip
            key={facet.category}
            active={classes.has(facet.category)}
            onClick={() => onToggleClass(facet.category)}
          >
            <span>{facet.category}</span>
            <SpreadBar facet={facet} />
            <span className="font-mono text-[10px] text-muted-foreground">{facet.count}</span>
          </FilterChip>
        ))}
      </FilterGroup>

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

// SpreadBar is the little allow/ask/deny distribution strip on a class chip.
function SpreadBar({ facet }: { facet: ClassFacet }) {
  const total = facet.count || 1;
  return (
    <span className="inline-flex h-1 w-6 overflow-hidden rounded-full bg-border">
      {facet.allow > 0 && (
        <span className="bg-emerald-500" style={{ flex: facet.allow / total }} />
      )}
      {facet.ask > 0 && <span className="bg-amber-500" style={{ flex: facet.ask / total }} />}
      {facet.deny > 0 && (
        <span className="bg-destructive" style={{ flex: facet.deny / total }} />
      )}
    </span>
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

function OriginTag({
  row,
  editLayer,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
}) {
  const origin = originOf(row, editLayer);
  return (
    <Badge
      variant="outline"
      title={
        origin.kind === "override"
          ? `This rule is an override set on ${origin.level}.`
          : `No override on this level — the rule is inherited from ${origin.level}.`
      }
      className={cn(
        "font-normal",
        origin.kind === "override"
          ? "border-primary/40 text-primary"
          : "text-muted-foreground",
      )}
    >
      {origin.label}
    </Badge>
  );
}

// DecisionControl is the single editable pill the redesign settled on (one clear
// control per row, not a four-button badge soup). The pill shows the EFFECTIVE
// verdict — what actually happens — coloured and with a Lock icon when a layer
// above this page capped it. Opening it writes this page's own layer; choosing
// Inherit clears that layer. A subtle ring marks rows this page has overridden.
function DecisionControl({
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
  const Icon = row.effective.capped_by ? Lock : VERDICT_ICON[verdict];
  const overridden = !!row.layers[editLayer];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={`Decision: ${SETTING_LABEL[verdict]}`}
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
}: {
  groups: RepoGroupData[];
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (url: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: RepoGroupData, setting: ToolSetting) => void;
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
}: {
  group: RepoGroupData;
  editLayer: ToolLayer;
  busy: boolean;
  onSetCapability: (url: string, toolKey: string, setting: ToolSetting) => void;
  onSetGroup: (group: RepoGroupData, setting: ToolSetting) => void;
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
