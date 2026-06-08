"use client";

import { cn } from "@multica/ui/lib/utils";
import { BarChart3, MessageSquare } from "lucide-react";
import { useDashboardStore, type DashboardTab } from "../../core/store";

const TABS: { id: DashboardTab; label: string; icon: React.ReactNode }[] = [
  { id: "issues", label: "Issues", icon: <BarChart3 className="size-3.5" /> },
  { id: "messages", label: "Beskeder", icon: <MessageSquare className="size-3.5" /> },
];

export function DashboardTabBar() {
  const tab = useDashboardStore((s) => s.tab);
  const setTab = useDashboardStore((s) => s.setTab);

  return (
    <div className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      {TABS.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => setTab(t.id)}
          className={cn(
            "flex items-center gap-1.5 rounded-sm px-2.5 py-1 text-xs font-medium transition-colors",
            t.id === tab
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {t.icon}
          {t.label}
        </button>
      ))}
    </div>
  );
}
