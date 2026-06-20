"use client";

// FIR-1609 Phase 5 — the per-data-source configuration sheet for the
// firtal_registry tool. It mirrors the connection "Konfigurer" sheet
// (connection-config-sheet.tsx), but one row per Firtal Data Registry data
// source instead of per MCP tool / API endpoint.
//
// The rows themselves come from the unified tool-policy table: the server folds
// each data source into a per-resource row under firtal_registry
// (source === "registry-data-source", resource_pattern = data-source id) so this
// sheet does NOT fetch — it edits the rows the table already holds. Each row gets
// the SAME Decision pill + When (Condition) control as every other permission row
// in the app, so a data source reads and writes exactly like a connection
// endpoint or a repo capability. Writes go to the same chain the daemon enforces:
// tool_key = "firtal_registry", resource_pattern = <data-source id>, at this
// surface's layer — so inheritance holds and the gate reads the same rows.

import { useMemo, useState } from "react";
import { Loader2, Search, Settings2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import {
  useClearToolPolicy,
  useSetToolPolicy,
  type ToolCondition,
  type ToolLayer,
  type ToolPolicyRow,
  type ToolSetting,
} from "../core/tool-policy";
import { DecisionControl, ConditionControl } from "./tool-policy-table";

export interface FirtalRegistryDataSourceSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The firtal_registry capability key — always "firtal_registry". */
  toolKey: string;
  /** Display label for the header (the tool's title). */
  toolLabel: string;
  /** The per-data-source rows (source === "registry-data-source"). */
  sourceRows: ToolPolicyRow[];
  /** The layer this surface authors and the subject those writes target. */
  editLayer: ToolLayer;
  subjectId: string;
}

export function FirtalRegistryDataSourceSheet({
  open,
  onOpenChange,
  toolKey,
  toolLabel,
  sourceRows,
  editLayer,
  subjectId,
}: FirtalRegistryDataSourceSheetProps) {
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();
  const [search, setSearch] = useState("");

  const busy = setPolicy.isPending || clearPolicy.isPending;

  const sorted = useMemo(
    () =>
      [...sourceRows].sort((a, b) =>
        (a.title || a.resource_pattern).localeCompare(
          b.title || b.resource_pattern,
        ),
      ),
    [sourceRows],
  );
  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return sorted;
    return sorted.filter((r) =>
      (r.title || r.resource_pattern).toLowerCase().includes(q),
    );
  }, [sorted, search]);

  // Write (or clear) one data source's decision at this surface's layer.
  function applySetting(resourcePattern: string, setting: ToolSetting) {
    const scope = { resource_pattern: resourcePattern };
    if (setting === "inherit") {
      clearPolicy.mutate({
        tool_key: toolKey,
        layer: editLayer,
        subject_id: subjectId,
        ...scope,
      });
      return;
    }
    setPolicy.mutate({
      tool_key: toolKey,
      layer: editLayer,
      subject_id: subjectId,
      setting,
      ...scope,
    });
  }

  // Write (or clear) the When (Condition) on one data source's rule. A condition
  // can only refine a CONCRETE rule (allow/ask/deny) already authored on this
  // layer — the same guard the main table makes — so a drifted row never creates
  // an override as a side effect. The Decision is resent unchanged.
  function applyCondition(row: ToolPolicyRow, condition: ToolCondition | null) {
    const setting = editLayer === "group" ? null : row.layers[editLayer];
    if (setting !== "allow" && setting !== "ask" && setting !== "deny") return;
    setPolicy.mutate({
      tool_key: toolKey,
      layer: editLayer,
      subject_id: subjectId,
      setting,
      condition,
      resource_pattern: row.resource_pattern,
    });
  }

  function allowAll() {
    for (const r of sourceRows) applySetting(r.resource_pattern, "allow");
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full max-w-xl flex-col gap-0 p-0 sm:max-w-xl"
      >
        <SheetHeader className="border-b">
          <SheetTitle>{toolLabel} — data sources</SheetTitle>
          <SheetDescription>
            Allow, ask or deny each Firtal Data Registry data source individually,
            and set a When-condition on any of them. A per-source choice can only
            tighten the firtal_registry setting, never loosen it.
          </SheetDescription>
        </SheetHeader>

        <div className="flex items-center gap-2 border-b p-4">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search data sources…"
              className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy || sourceRows.length === 0}
            onClick={allowAll}
          >
            {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            Tillad alle
          </Button>
        </div>

        <div className="flex flex-1 flex-col gap-1 overflow-y-auto p-4">
          {sourceRows.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No data sources configured for this workspace yet.
            </p>
          ) : filtered.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No data sources match the search.
            </p>
          ) : (
            filtered.map((r) => (
              <div
                key={r.resource_pattern}
                data-testid={`registry-data-source-${r.resource_pattern}`}
                className="flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">
                    {r.title || r.resource_pattern}
                  </div>
                  <div className="truncate font-mono text-xs text-muted-foreground">
                    {r.resource_pattern}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <DecisionControl
                    row={r}
                    editLayer={editLayer}
                    disabled={busy}
                    onChange={(s) => applySetting(r.resource_pattern, s)}
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

// FirtalRegistryDataSourceConfigure is the "Data sources (N)" button rendered on
// the firtal_registry capability row. It opens the per-source sheet, fed by the
// data-source rows the unified table already holds (no fetch). Renders nothing
// when there are no data-source rows (registry not configured, or not an
// agent-scoped view), so it never shows an empty affordance.
export function FirtalRegistryDataSourceConfigure({
  toolKey,
  toolLabel,
  sourceRows,
  editLayer,
  subjectId,
  variant = "outline",
  size = "sm",
}: {
  toolKey: string;
  toolLabel: string;
  sourceRows: ToolPolicyRow[];
  editLayer: ToolLayer;
  subjectId: string;
  variant?: "ghost" | "outline" | "secondary";
  size?: "sm" | "default";
}) {
  const [open, setOpen] = useState(false);
  if (sourceRows.length === 0) return null;

  return (
    <>
      <Button
        type="button"
        size={size}
        variant={variant}
        className="gap-1.5"
        onClick={() => setOpen(true)}
        data-testid="firtal-registry-data-sources-configure"
        title={`Configure data sources for ${toolLabel}`}
      >
        <Settings2 className="h-3.5 w-3.5" />
        Data sources ({sourceRows.length})
      </Button>
      <FirtalRegistryDataSourceSheet
        open={open}
        onOpenChange={setOpen}
        toolKey={toolKey}
        toolLabel={toolLabel}
        sourceRows={sourceRows}
        editLayer={editLayer}
        subjectId={subjectId}
      />
    </>
  );
}
