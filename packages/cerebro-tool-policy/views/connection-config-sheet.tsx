"use client";

// TECH-3156 — the per-tool configuration sheet for a workspace MCP connection.
//
// A connection (e.g. customer-service) exposes many underlying tools. The
// Connections tab shows one row per connection with a single Allow/Ask/Deny for
// the whole connection; this sheet — opened from the row's "Konfigurer" button —
// lets the admin allow or deny each underlying tool individually, exactly the
// firtal_registry pattern Jesper asked for.
//
// Decisions are written to the same tool-policy chain every other surface uses:
// tool_key = "connection:<name>", resource_pattern = <tool name>, at the layer
// this surface authors. So inheritance holds — a per-tool decision can only
// tighten the connection-wide one, and the daemon enforcement reads the exact
// same rows.

import { useMemo, useState } from "react";
import {
  ChevronDown,
  Loader2,
  Lock,
  Search,
  ShieldAlert,
  ShieldCheck,
  ShieldQuestion,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";
import {
  isLockedFromElsewhere,
  useSetToolPolicy,
  useClearToolPolicy,
  type ToolCondition,
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolSetting,
  type ToolPolicyRow,
} from "../core/tool-policy";
import { CatalogDecisionControl, ConditionControl } from "./tool-policy-table";

const CHOICES: ToolSetting[] = ["allow", "ask", "deny", "inherit"];
const CHOICE_LABEL: Record<ToolSetting, string> = {
  allow: "Allow",
  ask: "Ask",
  deny: "Deny",
  inherit: "Inherit",
};

// Restrictiveness rank — a per-tool choice can only TIGHTEN the connection-wide
// floor, never loosen it. Any concrete choice below the floor's rank is futile,
// so the sheet disables it (TECH-3287 hul 7). `inherit` is never futile: it
// clears the per-tool row so the tool simply follows the connection floor.
const SETTING_RANK: Record<ToolEffectiveSetting, number> = { allow: 0, ask: 1, deny: 2 };

// Decision palette + icons — identical to the main tool-policy table's pill so a
// REST endpoint row reads exactly like every other permission row in the app
// (TECH-3287 hul: Jesper's "use the same toggle as everywhere else"). The pill
// renders the EFFECTIVE verdict; the dropdown writes this surface's own layer.
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

const LAYER_LABEL: Record<string, string> = {
  workspace: "Workspace",
  runtime: "Runtime",
  agent: "Agent",
  group: "Group",
  user: "User",
};

// changeHint tells the admin WHERE to change the blocking layer (TECH-3287 hul 4
// copy, scoped to the connection sheet).
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
    default:
      return "";
  }
}

export interface ConnectionConfigSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The connection capability key, e.g. "connection:customer-service". */
  connectionKey: string;
  /** The connection's display name, shown in the header. */
  connectionLabel: string;
  /**
   * The connection-wide row (source === "connection"). Its Effective verdict is
   * the floor every per-tool row inherits — used to explain a block from above
   * and disable futile per-tool choices (TECH-3287 hul 1/6/7).
   */
  connectionRow: ToolPolicyRow;
  /** The per-tool rows for this connection (source === "connection-tool"). */
  toolRows: ToolPolicyRow[];
  /** The layer this surface authors and the subject those writes target. */
  editLayer: ToolLayer;
  subjectId: string;
}

export function ConnectionConfigSheet({
  open,
  onOpenChange,
  connectionKey,
  connectionLabel,
  connectionRow,
  toolRows,
  editLayer,
  subjectId,
}: ConnectionConfigSheetProps) {
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();
  const [search, setSearch] = useState("");

  const busy = setPolicy.isPending || clearPolicy.isPending;

  // The connection-wide Effective is the floor every per-tool row inherits. A
  // per-tool choice can only tighten it, so anything below it is futile.
  const floor = connectionRow.effective.setting;
  const floorRank = SETTING_RANK[floor];
  // Blocked from a higher layer this page can't loosen → drives the banner +
  // the lock icon (TECH-3287 hul 1/3).
  const blockedAbove = isLockedFromElsewhere(connectionRow, editLayer);
  // "Tillad alle" can never beat a connection-wide Ask/Deny → disable it when the
  // floor is not Allow (TECH-3287 hul 6).
  const allowAllFutile = floorRank > 0;

  const blocker = connectionRow.effective.capped_by || connectionRow.effective.decided_by;
  const blockerGroups = connectionRow.capped_by_groups
    .map((g) => (g.owner ? `${g.name} (owner: ${g.owner})` : g.name))
    .join(", ");
  const blockerLabel =
    blocker === "group" && blockerGroups
      ? `group ${blockerGroups}`
      : (LAYER_LABEL[blocker] ?? blocker);

  // API connections expose endpoint+method rows ("POST /orders"); MCP connections
  // expose tools. Adapt the copy so a REST connection doesn't read as "tools".
  const isApi = toolRows.some((r) => r.source === "connection-endpoint");
  const unitLabel = isApi ? "endpoint" : "tool";

  const sorted = useMemo(
    () =>
      [...toolRows].sort((a, b) =>
        (a.title || a.resource_pattern).localeCompare(
          b.title || b.resource_pattern,
        ),
      ),
    [toolRows],
  );
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return sorted;
    return sorted.filter(
      (r) =>
        (r.title || r.resource_pattern).toLowerCase().includes(q) ||
        r.resource_pattern.toLowerCase().includes(q),
    );
  }, [sorted, search]);

  function write(tool: string, setting: ToolSetting) {
    const scope = { resource_pattern: tool };
    if (setting === "inherit") {
      clearPolicy.mutate({
        tool_key: connectionKey,
        layer: editLayer,
        subject_id: subjectId,
        ...scope,
      });
      return;
    }
    setPolicy.mutate({
      tool_key: connectionKey,
      layer: editLayer,
      subject_id: subjectId,
      setting,
      ...scope,
    });
  }

  // Write (or clear) the When (Condition) on one tool's rule. A condition can
  // only refine a CONCRETE rule (allow/ask/deny) already authored on this layer —
  // the same guard the main table makes — so a drifted row never creates an
  // override as a side effect. The Decision is resent unchanged; connection tool
  // rows pick up the host facet automatically (conditionFacets).
  function applyCondition(row: ToolPolicyRow, condition: ToolCondition | null) {
    const setting = editLayer === "group" ? null : row.layers[editLayer];
    if (setting !== "allow" && setting !== "ask" && setting !== "deny") return;
    setPolicy.mutate({
      tool_key: connectionKey,
      layer: editLayer,
      subject_id: subjectId,
      setting,
      condition,
      resource_pattern: row.resource_pattern,
    });
  }

  function allowAll() {
    for (const r of toolRows) write(r.resource_pattern, "allow");
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        // Wide on purpose (FIR-2640): endpoint paths and their spec names must be
        // readable while granting — full screen on mobile, 70% of the viewport on
        // desktop. The width classes carry the data-[side=right] prefix so they
        // beat the base SheetContent's own data-[side=right]:w-3/4 /
        // sm:max-w-sm (a plain w-full loses to it on specificity, leaving the
        // sheet stuck at 75% on mobile — FIR-2640 review).
        className="flex flex-col gap-0 p-0 data-[side=right]:w-full data-[side=right]:max-w-full data-[side=right]:sm:w-[70vw] data-[side=right]:sm:max-w-[70vw]"
      >
        <SheetHeader className="border-b">
          <SheetTitle>
            {connectionLabel} — {isApi ? "endpoints" : "tools"}
          </SheetTitle>
          <SheetDescription>
            Allow or deny each {unitLabel} individually
            {isApi ? " (HTTP method + path)" : ""}. A per-{unitLabel} choice can
            only tighten the connection-wide setting, never loosen it.
          </SheetDescription>
        </SheetHeader>

        {blockedAbove ? (
          <div
            data-testid="connection-blocked-banner"
            className="flex items-start gap-2 border-b border-destructive/30 bg-destructive/10 px-4 py-3 text-xs text-destructive"
          >
            <Lock className="mt-0.5 h-4 w-4 shrink-0" />
            <div>
              <p className="font-medium">
                The whole connection is set to “{CHOICE_LABEL[floor]}” by {blockerLabel}.
              </p>
              <p className="mt-0.5 text-destructive/90">
                {floor === "deny"
                  ? `An Allow on a single ${unitLabel} here has no effect — the connection is blocked from a higher layer.`
                  : `A single ${unitLabel} can only be tightened (not made more open than the connection).`}{" "}
                {changeHint(blocker)}
              </p>
            </div>
          </div>
        ) : null}

        <div className="flex items-center gap-2 border-b p-4">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={`Search ${unitLabel}s…`}
              className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy || toolRows.length === 0 || allowAllFutile}
            onClick={allowAll}
            title={
              allowAllFutile
                ? `The connection is set to “${CHOICE_LABEL[floor]}” — Allow all has no effect.`
                : undefined
            }
          >
            {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            Allow all
          </Button>
        </div>

        {/* overflow-auto + an inner min-w-max column let long endpoint paths /
            tool names scroll horizontally on a narrow (mobile) sheet instead of
            truncating to nothing (FIR-2640 review). */}
        <div className="flex-1 overflow-auto p-4">
          <div className="flex min-w-max flex-col gap-1">
          {toolRows.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {isApi
                ? "No endpoints configured yet. Add endpoint permissions on the connection, then reopen this sheet."
                : "No tools discovered yet. Test the connection to fetch its tool list, then reopen this sheet."}
            </p>
          ) : filtered.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No {unitLabel}s match the search.
            </p>
          ) : (
            filtered.map((r) => (
              <div
                key={r.resource_pattern}
                data-testid={`connection-tool-${r.resource_pattern}`}
                className="flex w-full items-center justify-between gap-4 rounded-md border px-3 py-2"
              >
                <div className="min-w-0">
                  <div
                    className={`whitespace-nowrap text-sm ${!r.title || r.title === r.resource_pattern ? "font-mono" : "font-medium"}`}
                  >
                    {r.title || r.resource_pattern}
                  </div>
                  {r.title && r.title !== r.resource_pattern ? (
                    <div className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                      {r.resource_pattern}
                    </div>
                  ) : null}
                  {r.effective.capped_by ? (
                    <div className="text-xs text-muted-foreground">
                      Capped by {r.effective.capped_by}
                    </div>
                  ) : null}
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <EndpointDecisionControl
                    row={r}
                    editLayer={editLayer}
                    disabled={busy}
                    floorRank={floorRank}
                    floorLabel={CHOICE_LABEL[floor]}
                    onChange={(setting) => write(r.resource_pattern, setting)}
                  />
                  <ConditionControl
                    row={r}
                    editLayer={editLayer}
                    disabled={busy}
                    onChange={(c) => applyCondition(r, c)}
                  />
                </div>
              </div>
            ))
          )}
          </div>
        </div>

        <SheetFooter className="border-t">
          <div className="flex justify-end">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Luk
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

// ConnectionToolList (FIR-2706) renders a connection's per-tool rows as an inline
// list — the SAME rows the sheet shows, but mounted directly under an expanded
// connection row in the capability catalog instead of inside a right-side Sheet.
// It renders CatalogDecisionControl (the catalog's single decision-with-When
// pill, same as repo and credential sub-rows) with the identical tighten-only
// write semantics, so inline editing and sheet editing can never drift. The
// wide Sheet stays for the classic table; this is the redesign's "expand and
// show the group" surface Jesper asked for.
export function ConnectionToolList({
  connectionKey,
  connectionRow,
  toolRows,
  editLayer,
  subjectId,
}: {
  connectionKey: string;
  connectionRow: ToolPolicyRow;
  toolRows: ToolPolicyRow[];
  editLayer: ToolLayer;
  subjectId: string;
}) {
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();
  const busy = setPolicy.isPending || clearPolicy.isPending;

  // The connection-wide Effective is the floor every per-tool row inherits; a
  // per-tool choice can only tighten it (TECH-3287 hul 7), same as the sheet.
  const floor = connectionRow.effective.setting;
  const floorRank = SETTING_RANK[floor];
  const isApi = toolRows.some((r) => r.source === "connection-endpoint");
  const unitLabel = isApi ? "endpoint" : "tool";

  const sorted = useMemo(
    () =>
      [...toolRows].sort((a, b) =>
        (a.title || a.resource_pattern).localeCompare(
          b.title || b.resource_pattern,
        ),
      ),
    [toolRows],
  );

  // Write (or clear) one tool's decision at this surface's layer — verbatim the
  // sheet's write(), so the two entry points behave identically.
  function write(tool: string, setting: ToolSetting) {
    const scope = { resource_pattern: tool };
    if (setting === "inherit") {
      clearPolicy.mutate({ tool_key: connectionKey, layer: editLayer, subject_id: subjectId, ...scope });
      return;
    }
    setPolicy.mutate({ tool_key: connectionKey, layer: editLayer, subject_id: subjectId, setting, ...scope });
  }

  function applyCondition(row: ToolPolicyRow, condition: ToolCondition | null) {
    const setting = editLayer === "group" ? null : row.layers[editLayer];
    if (setting !== "allow" && setting !== "ask" && setting !== "deny") return;
    setPolicy.mutate({
      tool_key: connectionKey,
      layer: editLayer,
      subject_id: subjectId,
      setting,
      condition,
      resource_pattern: row.resource_pattern,
    });
  }

  if (sorted.length === 0) {
    return (
      <p className="py-3 text-center text-xs text-muted-foreground">
        {isApi
          ? "No endpoints configured yet. Test the connection to fetch them."
          : "No tools discovered yet. Test the connection to fetch its tool list."}
      </p>
    );
  }

  return (
    // FIR-2706 — a full-width divided list flush to the expanded panel, not a stack
    // of inset bordered cards. Each row spends its width on the tool name, not on
    // padding + a per-card margin, so long names survive on mobile; the controls
    // wrap under the name on a narrow row instead of crushing it.
    <div
      className="flex flex-col divide-y overflow-hidden rounded-md border bg-background"
      data-testid={`connection-tool-list-${connectionKey}`}
    >
      {sorted.map((r) => (
        <div
          key={r.resource_pattern}
          data-testid={`connection-tool-${r.resource_pattern}`}
          className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5 px-2.5 py-2"
        >
          <div className="min-w-0 flex-1 basis-40">
            <div
              className={`truncate text-sm ${!r.title || r.title === r.resource_pattern ? "font-mono" : "font-medium"}`}
            >
              {r.title || r.resource_pattern}
            </div>
            {r.title && r.title !== r.resource_pattern ? (
              <div className="truncate font-mono text-xs text-muted-foreground">
                {r.resource_pattern}
              </div>
            ) : null}
            {r.effective.capped_by ? (
              <div className="text-xs text-muted-foreground">
                Capped by {r.effective.capped_by}
              </div>
            ) : null}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {/* The SAME single pill as every other catalog sub-row (repos,
                credential groups): decision + When inside one control
                (FIR-2706 follow-up — "100% identical everywhere"). The
                floorRank/floorLabel pair keeps the sheet's tighten-only rule. */}
            <CatalogDecisionControl
              row={r}
              editLayer={editLayer}
              disabled={busy}
              floorRank={floorRank}
              floorLabel={CHOICE_LABEL[floor]}
              onDecision={(setting) => write(r.resource_pattern, setting)}
              onCondition={(c) => applyCondition(r, c)}
            />
          </div>
        </div>
      ))}
      <span className="sr-only">{unitLabel} list</span>
    </div>
  );
}

// EndpointDecisionControl is the single editable pill — one compact control per
// endpoint row instead of the old four-button row that overflowed on mobile and
// read differently from the rest of the app. It mirrors the main tool-policy
// table's DecisionControl: the pill shows the EFFECTIVE verdict (coloured), and
// opening it writes this surface's own layer. A per-endpoint choice can only
// TIGHTEN the connection-wide floor, so looser choices are disabled in the menu
// (TECH-3287 hul 7). Choosing Inherit clears this surface's row.
function EndpointDecisionControl({
  row,
  editLayer,
  disabled,
  floorRank,
  floorLabel,
  onChange,
}: {
  row: ToolPolicyRow;
  editLayer: ToolLayer;
  disabled?: boolean;
  /** Restrictiveness rank of the connection-wide floor; looser choices are futile. */
  floorRank: number;
  /** Display label of the connection-wide floor, for the futile-choice tooltip. */
  floorLabel: string;
  onChange: (setting: ToolSetting) => void;
}) {
  const verdict = row.effective.setting;
  const layerSetting = row.layers[editLayer] ?? "inherit";
  const overridden = !!row.layers[editLayer];
  const Icon = VERDICT_ICON[verdict];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        disabled={disabled}
        aria-label={`Decision: ${CHOICE_LABEL[verdict]}`}
        className={cn(
          "inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold transition-colors disabled:opacity-50",
          VERDICT_PILL[verdict],
          overridden && "ring-1 ring-primary/40",
        )}
      >
        <Icon className="size-3.5" />
        {CHOICE_LABEL[verdict]}
        <ChevronDown className="size-3 opacity-60" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-36">
        {CHOICES.map((choice) => {
          const futile =
            choice !== "inherit" && SETTING_RANK[choice] < floorRank;
          return (
            <DropdownMenuItem
              key={choice}
              disabled={futile}
              onClick={() => onChange(choice)}
              title={
                futile
                  ? `The connection is set to “${floorLabel}” — “${CHOICE_LABEL[choice]}” here is more open and has no effect.`
                  : undefined
              }
              className={cn(
                "text-sm",
                choice === "inherit" && "text-muted-foreground",
                layerSetting === choice && "font-semibold",
              )}
            >
              {CHOICE_LABEL[choice]}
              {choice === "inherit" && (
                <span className="ml-auto text-xs text-muted-foreground">
                  clears {LAYER_LABEL[editLayer] ?? editLayer}
                </span>
              )}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
