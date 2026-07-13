import type { ReactNode } from "react";
import type { AgentPageTab, RedesignTab } from "./agent-page-tabs";

export function AdvancedTabs({
  tabs,
  active,
  onSelect,
  renderContent,
}: {
  tabs: AgentPageTab[];
  active: RedesignTab;
  onSelect: (tab: RedesignTab) => void;
  renderContent: (tab: RedesignTab) => ReactNode;
}) {
  const effective = tabs.some((tab) => tab.id === active) ? active : tabs[0]?.id;

  if (!effective) return null;

  return (
    <div className="min-h-[70vh]">
      <div role="tablist" aria-label="Advanced settings" className="flex gap-1 overflow-x-auto border-b bg-background px-4 pt-3">
        {tabs.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={effective === id}
            onClick={() => onSelect(id)}
            className={`flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2.5 text-sm font-medium ${
              effective === id
                ? "border-foreground text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            <Icon className="h-4 w-4" />
            {label}
          </button>
        ))}
      </div>
      <div role="tabpanel">{renderContent(effective)}</div>
    </div>
  );
}
