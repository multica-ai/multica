"use client";

import { cn } from "@multica/ui/lib/utils";
import { useDashboardStore, type DashboardTab } from "../../core/store";

const TABS: { id: DashboardTab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "runs", label: "Runs" },
  { id: "messages", label: "Messages" },
];

export function DashboardTabBar() {
  const tab = useDashboardStore((s) => s.tab);
  const setTab = useDashboardStore((s) => s.setTab);

  return (
    <nav aria-label="Dashboard sections" className="inline-flex h-full items-stretch gap-6">
      {TABS.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => setTab(t.id)}
          className={cn(
            "flex items-center border-b-2 px-0 text-xs font-medium transition-colors",
            t.id === tab
              ? "border-[#6557d8] text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground",
          )}
        >
          {t.label}
        </button>
      ))}
    </nav>
  );
}
