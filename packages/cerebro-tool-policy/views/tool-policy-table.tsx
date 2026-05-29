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
}

// VIEW_EDIT_LAYER maps each surface to the chain layer it authors. The member
// page edits the User (ceiling) layer — "member" is the page, "user" is the rung.
const VIEW_EDIT_LAYER: Record<ToolPolicyView, ToolLayer> = {
  workspace: "workspace",
  runtime: "runtime",
  agent: "agent",
  group: "group",
  member: "user",
};

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
  const facets = useMemo(() => classFacets(rows), [rows]);
  const filtered = useMemo(
    () => filterRows(rows, { search, classes, effects, decisions, showInherited, editLayer }),
    [rows, search, classes, effects, decisions, showInherited, editLayer],
  );

  const busy = setPolicy.isPending || clearPolicy.isPending;

  function applySetting(toolKey: string, setting: ToolSetting) {
    if (setting === "inherit") {
      clearPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId });
      return;
    }
    setPolicy.mutate({ tool_key: toolKey, layer: editLayer, subject_id: subjectId, setting });
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
        total={rows.length}
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
      ) : filtered.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {rows.length === 0 ? "No tools reported yet." : "No tools match these filters."}
        </p>
      ) : (
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
                      <Badge variant="outline" className="font-normal">
                        {row.category || "Uncategorised"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <SideEffectTag effect={classifySideEffect(row)} />
                    </TableCell>
                    <TableCell>
                      <DecisionControl
                        row={row}
                        editLayer={editLayer}
                        disabled={busy}
                        onChange={(s) => applySetting(row.tool_key, s)}
                      />
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
                  <DecisionControl
                    row={row}
                    editLayer={editLayer}
                    disabled={busy}
                    onChange={(s) => applySetting(row.tool_key, s)}
                  />
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-1.5">
                  <Badge variant="outline" className="font-normal">
                    {row.category || "Uncategorised"}
                  </Badge>
                  <SideEffectTag effect={classifySideEffect(row)} />
                  <OriginTag row={row} editLayer={editLayer} />
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
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
