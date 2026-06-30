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
// Each section is a SELF-CONTAINED tabbed card: it remembers which of its own
// tabs is showing and renders that tab's content INSIDE its own card — tab
// strip on top, content directly below, as one box. So all three cards display
// their own tabs + content at the same time, matching the requested
// "Tabs / Indhold" per-card layout (FIR-2280 followup: previously a single
// globally-active tab meant only one card carried content and the other two
// showed bare tab strips, which read as "three rows of tabs").
//
// The pane still owns one "focus" tab (for sibling nav-intent and the
// unsaved-changes guard); when it changes we point its owning section at it and
// open that section. Advanced is collapsed by default so its (heavier, editable)
// tabs only mount once the user expands it.
//
// Lives in the cerebro zone so the heavy layout logic stays out of the
// upstream file — agent-overview-pane only carries a thin marked import + use,
// and passes a `renderContent(tabId)` function so each card can render its own.

import { useEffect, useState, type ComponentType, type ReactNode } from "react";
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
  renderContent,
}: {
  tabs: AgentTabSectionItem[];
  /** The pane's focus tab: when it changes, its owning section shows it. */
  activeTab: string;
  onSelect: (id: string) => void;
  /** Renders a given tab's content; each card calls this for its own tab. */
  renderContent: (tabId: string) => ReactNode;
}) {
  const [openSections, setOpenSections] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(SECTIONS.map((s) => [s.id, s.defaultOpen])),
  );
  // Each section remembers which of its own tabs is showing, so all three
  // cards can carry their own content at once instead of just the one globally
  // active section. Seeded lazily; the focus effect below keeps it in sync.
  const [shownBySection, setShownBySection] = useState<Record<string, string>>(
    {},
  );

  // When the pane focuses a tab (initial load, a sibling's nav-intent, or a
  // committed tab click), point its owning section at it and open that section.
  // Routing clicks through the pane — not setting section state directly — is
  // what preserves the unsaved-changes guard: a blocked change never lands here.
  useEffect(() => {
    if (!activeTab) return;
    const owner = sectionForTab(activeTab);
    setShownBySection((prev) =>
      prev[owner] === activeTab ? prev : { ...prev, [owner]: activeTab },
    );
    setOpenSections((prev) => (prev[owner] ? prev : { ...prev, [owner]: true }));
  }, [activeTab]);

  return (
    // This stack is the scrollable body of the pane: each section is its own
    // self-contained card — its tab strip and the content of its shown tab.
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
        const firstTab = sectionTabs[0];
        if (!firstTab) return null;

        // This card's own shown tab: the remembered one if it's still present,
        // else the section's first tab. Each card renders its own content.
        const shownTab =
          sectionTabs.find((tab) => tab.id === shownBySection[section.id])?.id ??
          firstTab.id;
        const open = openSections[section.id] ?? section.defaultOpen;
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
                      shownTab === tab.id
                        ? "border-foreground text-foreground"
                        : "border-transparent text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    <tab.icon className="h-3.5 w-3.5" />
                    {tab.label}
                  </button>
                ))}
              </div>
              {/* This card's own shown-tab content sits with its tab strip. */}
              <div className="border-t bg-background">
                {renderContent(shownTab)}
              </div>
            </CollapsibleContent>
          </Collapsible>
        );
      })}
    </div>
  );
}
