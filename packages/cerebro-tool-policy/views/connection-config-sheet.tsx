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
import { Loader2, Lock, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
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
  type ToolEffectiveSetting,
  type ToolLayer,
  type ToolSetting,
  type ToolPolicyRow,
} from "../core/tool-policy";

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
      return "Ændr det under Settings → Tools (workspace).";
    case "runtime":
      return "Ændr det på Runtime-indstillinger → Tools.";
    case "agent":
      return "Ændr det på denne agents Tools-fane.";
    case "group":
      return "Ændr det under Settings → Groups.";
    case "user":
      return "Det er sat på brugerens egne rettigheder (loftet).";
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
  // A concrete per-tool choice looser than the floor is futile (TECH-3287 hul 7).
  const choiceFutile = (choice: ToolSetting) =>
    choice !== "inherit" && SETTING_RANK[choice] < floorRank;

  const blocker = connectionRow.effective.capped_by || connectionRow.effective.decided_by;
  const blockerGroups = connectionRow.capped_by_groups
    .map((g) => (g.owner ? `${g.name} (ejer: ${g.owner})` : g.name))
    .join(", ");
  const blockerLabel =
    blocker === "group" && blockerGroups
      ? `gruppen ${blockerGroups}`
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
    return sorted.filter((r) =>
      (r.title || r.resource_pattern).toLowerCase().includes(q),
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

  function allowAll() {
    for (const r of toolRows) write(r.resource_pattern, "allow");
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full max-w-xl flex-col gap-0 p-0 sm:max-w-xl"
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
                Hele forbindelsen er sat til “{CHOICE_LABEL[floor]}” af {blockerLabel}.
              </p>
              <p className="mt-0.5 text-destructive/90">
                {floor === "deny"
                  ? `En Allow på et enkelt ${unitLabel} her får ingen effekt — forbindelsen er blokeret ovenfra.`
                  : `Et enkelt ${unitLabel} kan kun strammes (ikke gøres mere åbent end forbindelsen).`}{" "}
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
                ? `Forbindelsen er sat til “${CHOICE_LABEL[floor]}” — Tillad alle får ingen effekt.`
                : undefined
            }
          >
            {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            Tillad alle
          </Button>
        </div>

        <div className="flex flex-1 flex-col gap-1 overflow-y-auto p-4">
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
            filtered.map((r) => {
              const layerSetting = r.layers[editLayer] ?? "inherit";
              return (
                <div
                  key={r.resource_pattern}
                  data-testid={`connection-tool-${r.resource_pattern}`}
                  className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
                >
                  <div className="min-w-0">
                    <div className="truncate font-mono text-sm">
                      {r.title || r.resource_pattern}
                    </div>
                    <div className="text-xs text-muted-foreground">
                      Effective: {CHOICE_LABEL[r.effective.setting]}
                      {r.effective.capped_by
                        ? ` (capped by ${r.effective.capped_by})`
                        : ""}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    {CHOICES.map((choice) => {
                      const futile = choiceFutile(choice);
                      return (
                        <button
                          key={choice}
                          type="button"
                          disabled={busy || futile}
                          onClick={() => write(r.resource_pattern, choice)}
                          title={
                            futile
                              ? `Forbindelsen er sat til “${CHOICE_LABEL[floor]}” — “${CHOICE_LABEL[choice]}” her er mere åbent og får ingen effekt.`
                              : undefined
                          }
                          className={cn(
                            "rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-50",
                            layerSetting === choice
                              ? "border-primary bg-primary/10 text-primary"
                              : "text-muted-foreground hover:text-foreground",
                          )}
                        >
                          {CHOICE_LABEL[choice]}
                        </button>
                      );
                    })}
                  </div>
                </div>
              );
            })
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
