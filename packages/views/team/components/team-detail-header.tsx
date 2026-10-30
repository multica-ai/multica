"use client";

import type { ReactNode } from "react";
import { ChevronDown, PanelRight } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { teamHref, type TeamSelectionKind, type TeamTab } from "../selection";

/**
 * One header for every Team object: its name — a menu of what you can do with
 * it — and status, the object's tabs, then actions. Tabs are links so they
 * deep-link and survive refresh; a single tab is not a choice, so it is not
 * rendered.
 */
export function TeamDetailHeader({
  kind,
  id,
  tabs,
  activeTab,
  icon,
  name,
  menu,
  status,
  counts,
  actions,
  panelOpen,
  onTogglePanel,
}: {
  kind: TeamSelectionKind;
  id: string;
  tabs: TeamTab[];
  activeTab: TeamTab;
  icon: ReactNode;
  name: string;
  /** Items of the name menu; without them the name is plain text. */
  menu?: ReactNode;
  status?: ReactNode;
  counts?: Partial<Record<TeamTab, number>>;
  actions?: ReactNode;
  panelOpen?: boolean;
  onTogglePanel?: () => void;
}) {
  const { t } = useT("team");
  const wsPaths = useWorkspacePaths();
  const title = (
    <>
      {icon}
      <h2 className="truncate text-body font-semibold">{name}</h2>
    </>
  );
  return (
    <PageHeader className="gap-3">
      <div className="flex min-w-0 shrink items-center gap-2">
        {menu ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button
                  type="button"
                  className="-ml-1.5 flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 transition-colors hover:bg-surface-hover data-[popup-open]:bg-surface-hover"
                />
              }
            >
              {title}
              <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-56">
              {menu}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          title
        )}
        {status}
      </div>
      {tabs.length > 1 && (
        <nav className="flex shrink-0 items-center gap-1" aria-label={t(($) => $.detail.tabs_aria)}>
          {tabs.map((tab) => {
            const count = counts?.[tab];
            return (
              <AppLink
                key={tab}
                href={teamHref(wsPaths.team(), { kind, id, tab })}
                aria-current={tab === activeTab ? "page" : undefined}
                className={cn(
                  "flex h-7 items-center gap-1.5 rounded-md px-2 text-body transition-colors",
                  tab === activeTab
                    ? "bg-surface-selected font-medium text-surface-selected-foreground hover:bg-surface-selected"
                    : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
                )}
              >
                {t(($) => $.tabs[tab])}
                {count != null && count > 0 && (
                  <span className="text-caption font-normal tabular-nums text-muted-foreground">{count}</span>
                )}
              </AppLink>
            );
          })}
        </nav>
      )}
      <div className="flex flex-1 items-center justify-end gap-1">
        {actions}
        {onTogglePanel && (
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label={t(($) => $.detail.toggle_panel)}
            aria-pressed={panelOpen}
            onClick={onTogglePanel}
            className={cn(panelOpen && "bg-surface-selected")}
          >
            <PanelRight className="size-4" />
          </Button>
        )}
      </div>
    </PageHeader>
  );
}
