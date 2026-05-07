"use client";

import { cn } from "@multica/ui/lib/utils";
import { useDashboardStore } from "../../core/store";
import type { ActorScope } from "../../core/types";

const TABS: { value: ActorScope; label: string }[] = [
  { value: "all", label: "Alle" },
  { value: "members", label: "Members" },
  { value: "agents", label: "Agents" },
];

export function ActorTabs() {
  const scope = useDashboardStore((s) => s.scope);
  const setScope = useDashboardStore((s) => s.setScope);

  return (
    <div
      role="tablist"
      aria-label="Dashboard actor scope"
      className="inline-flex items-center rounded-md border bg-background p-0.5"
    >
      {TABS.map((t) => {
        const active = scope === t.value;
        return (
          <button
            key={t.value}
            role="tab"
            aria-selected={active}
            type="button"
            onClick={() => setScope(t.value)}
            className={cn(
              "rounded-sm px-3 py-1 text-xs font-medium transition-colors",
              active
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {t.label}
          </button>
        );
      })}
    </div>
  );
}
