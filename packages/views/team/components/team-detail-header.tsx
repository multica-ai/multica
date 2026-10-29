"use client";

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { teamHref, type TeamSelectionKind, type TeamTab } from "../selection";

/**
 * One header for every Team object: identity on the left, the object's tabs
 * next to it, actions on the right. Tabs are links so they deep-link and
 * survive refresh; a single tab is not a choice, so it is not rendered.
 */
export function TeamDetailHeader({
  kind,
  id,
  tabs,
  activeTab,
  identity,
  actions,
}: {
  kind: TeamSelectionKind;
  id: string;
  tabs: TeamTab[];
  activeTab: TeamTab;
  identity: ReactNode;
  actions?: ReactNode;
}) {
  const { t } = useT("team");
  const wsPaths = useWorkspacePaths();
  return (
    <PageHeader className="gap-3">
      <div className="flex min-w-0 shrink items-center gap-2">{identity}</div>
      {tabs.length > 1 && (
        <nav className="flex shrink-0 items-center gap-1" aria-label={t(($) => $.detail.tabs_aria)}>
          {tabs.map((tab) => (
            <AppLink
              key={tab}
              href={teamHref(wsPaths.team(), { kind, id, tab })}
              aria-current={tab === activeTab ? "page" : undefined}
              className={cn(
                "flex h-7 items-center rounded-md px-2 text-body transition-colors",
                tab === activeTab
                  ? "bg-surface-selected font-medium text-surface-selected-foreground hover:bg-surface-selected"
                  : "text-muted-foreground hover:bg-surface-hover hover:text-foreground",
              )}
            >
              {t(($) => $.tabs[tab])}
            </AppLink>
          ))}
        </nav>
      )}
      <div className="flex flex-1 items-center justify-end gap-1">{actions}</div>
    </PageHeader>
  );
}
