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
import { Loader2, Search } from "lucide-react";
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
  useSetToolPolicy,
  useClearToolPolicy,
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

export interface ConnectionConfigSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The connection capability key, e.g. "connection:customer-service". */
  connectionKey: string;
  /** The connection's display name, shown in the header. */
  connectionLabel: string;
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
  toolRows,
  editLayer,
  subjectId,
}: ConnectionConfigSheetProps) {
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();
  const [search, setSearch] = useState("");

  const busy = setPolicy.isPending || clearPolicy.isPending;

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
          <SheetTitle>{connectionLabel} — tools</SheetTitle>
          <SheetDescription>
            Allow or deny each underlying tool individually. A per-tool choice
            can only tighten the connection-wide setting, never loosen it.
          </SheetDescription>
        </SheetHeader>

        <div className="flex items-center gap-2 border-b p-4">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search tools…"
              className="h-8 w-full rounded-md border bg-background pl-7 pr-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
            />
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy || toolRows.length === 0}
            onClick={allowAll}
          >
            {busy ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : null}
            Tillad alle
          </Button>
        </div>

        <div className="flex flex-1 flex-col gap-1 overflow-y-auto p-4">
          {toolRows.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No tools discovered yet. Test the connection to fetch its tool
              list, then reopen this sheet.
            </p>
          ) : filtered.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No tools match the search.
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
                    {CHOICES.map((choice) => (
                      <button
                        key={choice}
                        type="button"
                        disabled={busy}
                        onClick={() => write(r.resource_pattern, choice)}
                        className={cn(
                          "rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-50",
                          layerSetting === choice
                            ? "border-primary bg-primary/10 text-primary"
                            : "text-muted-foreground hover:text-foreground",
                        )}
                      >
                        {CHOICE_LABEL[choice]}
                      </button>
                    ))}
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
