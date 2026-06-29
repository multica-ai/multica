"use client";

// Agent Office (FIR-1775) — the collapsible tab-section shell for the agent
// detail page. The upstream agent-overview-pane renders a single flat strip of
// tab buttons; this component groups those same buttons into three labelled,
// collapsible sections per the approved v2 layout:
//
//   - Activity (open)   — Activity / Tasks / Capabilities
//   - Setup    (open)   — Instructions / Skills / Tools
//   - Advanced (closed) — everything else (Secrets / Custom Args / Sandbox / MCP / …)
//
// One tab is active across all sections; collapsing a section only hides its
// button row, never the active tab's content (the content area stays in the
// upstream pane). Advanced is collapsed by default so the page opens on the
// surfaces people actually visit.
//
// Lives in the cerebro zone so the heavy layout logic stays out of the
// upstream file — agent-overview-pane only carries a thin marked import + use.

import { useState, type ComponentType } from "react";
import { ChevronDown } from "lucide-react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";

export interface AgentTabSectionItem {
  id: string;
  icon: ComponentType<{ className?: string }>;
  /** Already-resolved (translated) label — i18n stays in the upstream pane. */
  label: string;
}

type SectionId = "activity" | "setup" | "advanced";

const SECTIONS: {
  id: SectionId;
  title: string;
  defaultOpen: boolean;
  tabIds: string[];
}[] = [
  {
    id: "activity",
    title: "Activity",
    defaultOpen: true,
    tabIds: ["activity", "tasks", "capabilities"],
  },
  {
    id: "setup",
    title: "Setup",
    defaultOpen: true,
    tabIds: ["instructions", "skills", "tools"],
  },
  {
    id: "advanced",
    title: "Advanced",
    defaultOpen: false,
    tabIds: ["env", "infisical", "custom_args", "sandbox", "mcp_config", "integrations"],
  },
];

// Any visible tab whose id isn't claimed by a section above falls through to
// Advanced, so a newly added tab can never silently disappear from the page.
function sectionForTab(id: string): SectionId {
  const owner = SECTIONS.find((s) => s.tabIds.includes(id));
  return owner ? owner.id : "advanced";
}

export function AgentTabSections({
  tabs,
  activeTab,
  onSelect,
}: {
  tabs: AgentTabSectionItem[];
  activeTab: string;
  onSelect: (id: string) => void;
}) {
  const [openSections, setOpenSections] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(SECTIONS.map((s) => [s.id, s.defaultOpen])),
  );

  return (
    <div className="flex shrink-0 flex-col border-b">
      {SECTIONS.map((section) => {
        // Preserve the section's declared tab order, then append any
        // unclaimed tabs that fell through to this section (Advanced only).
        const declared = section.tabIds
          .map((id) => tabs.find((tab) => tab.id === id))
          .filter((tab): tab is AgentTabSectionItem => Boolean(tab));
        const fellThrough = tabs.filter(
          (tab) =>
            sectionForTab(tab.id) === section.id &&
            !section.tabIds.includes(tab.id),
        );
        const sectionTabs = [...declared, ...fellThrough];
        if (sectionTabs.length === 0) return null;

        const open = openSections[section.id] ?? section.defaultOpen;
        return (
          <Collapsible
            key={section.id}
            open={open}
            onOpenChange={(next) =>
              setOpenSections((prev) => ({ ...prev, [section.id]: next }))
            }
            className="border-b last:border-b-0"
          >
            <CollapsibleTrigger className="flex w-full items-center gap-1.5 px-3 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground transition-colors hover:text-foreground md:px-4">
              <ChevronDown
                className={`h-3.5 w-3.5 shrink-0 transition-transform ${
                  open ? "" : "-rotate-90"
                }`}
              />
              {section.title}
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="flex flex-wrap items-center gap-0 px-1 pb-1 md:px-2">
                {sectionTabs.map((tab) => (
                  <button
                    key={tab.id}
                    type="button"
                    onClick={() => onSelect(tab.id)}
                    className={`flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2 text-xs font-medium transition-colors ${
                      activeTab === tab.id
                        ? "border-foreground text-foreground"
                        : "border-transparent text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    <tab.icon className="h-3.5 w-3.5" />
                    {tab.label}
                  </button>
                ))}
              </div>
            </CollapsibleContent>
          </Collapsible>
        );
      })}
    </div>
  );
}
