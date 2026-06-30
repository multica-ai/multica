"use client";

// Agent Office (FIR-1775, FIR-2280) — the collapsible tab-section shell for the
// agent detail page. The upstream agent-overview-pane renders a single flat
// strip of tab buttons; this component groups those same buttons into three
// labelled, collapsible sections, each rendered as its own separate card so the
// three groups read as three distinct boxes rather than rows packed into one:
//
//   - Activity (open)   — Activity / Tasks
//   - Context  (open)   — Instructions / Skills / Tools / Capabilities
//   - Advanced (closed) — everything else (Secrets / Custom Args / Sandbox / MCP / …)
//
// One tab is active across all sections. The active tab's content renders
// INSIDE the card whose group owns that tab — tab strip on top, its content
// directly below, as a single box (FIR-2280 followup: previously the content
// sat in one shared box below all three cards, which read as disconnected).
// The section that owns the active tab is forced open so its content can never
// be hidden; the other two cards show just their tab strips. Advanced is
// collapsed by default so the page opens on the surfaces people actually visit.
//
// Lives in the cerebro zone so the heavy layout logic stays out of the
// upstream file — agent-overview-pane only carries a thin marked import + use,
// and passes the active tab's content node in as `content`.

import { useState, type ComponentType, type ReactNode } from "react";
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
    tabIds: ["activity", "tasks"],
  },
  {
    id: "setup",
    title: "Context",
    defaultOpen: true,
    tabIds: ["instructions", "skills", "tools", "capabilities"],
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
  content,
}: {
  tabs: AgentTabSectionItem[];
  activeTab: string;
  onSelect: (id: string) => void;
  /** Rendered content of the active tab, placed inside its owning section. */
  content: ReactNode;
}) {
  const [openSections, setOpenSections] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(SECTIONS.map((s) => [s.id, s.defaultOpen])),
  );

  const activeSection = sectionForTab(activeTab);

  return (
    // This stack is the scrollable body of the pane: each section is its own
    // card, and the active tab's content lives inside the card that owns it.
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-2 md:gap-3 md:p-3">
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

        const ownsActive = activeSection === section.id;
        // The section holding the active tab is always open — collapsing it
        // would hide the very content the user is looking at.
        const open = (openSections[section.id] ?? section.defaultOpen) || ownsActive;
        return (
          <Collapsible
            key={section.id}
            open={open}
            onOpenChange={(next) =>
              setOpenSections((prev) => ({ ...prev, [section.id]: next }))
            }
            className="flex shrink-0 flex-col overflow-hidden rounded-lg border bg-muted/20"
          >
            <CollapsibleTrigger className="flex w-full items-center gap-1.5 px-3 py-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground transition-colors hover:text-foreground md:px-4">
              <ChevronDown
                className={`h-3.5 w-3.5 shrink-0 transition-transform ${
                  open ? "" : "-rotate-90"
                }`}
              />
              {section.title}
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="flex flex-wrap items-center gap-0 px-1.5 pb-1.5 md:px-2">
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
              {/* The active tab's content sits in the same card as its tabs. */}
              {ownsActive && (
                <div className="border-t bg-background">{content}</div>
              )}
            </CollapsibleContent>
          </Collapsible>
        );
      })}
    </div>
  );
}
