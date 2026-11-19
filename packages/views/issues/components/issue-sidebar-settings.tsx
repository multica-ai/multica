"use client";

import { Fragment, useRef, useState, type ReactElement, type ReactNode } from "react";
import { EyeOff, RotateCcw, SlidersHorizontal } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import {
  ISSUE_SIDEBAR_ALWAYS_SHOWN,
  ISSUE_SIDEBAR_GROUPS,
  selectIssueSidebarCustomized,
  useIssueSidebarSectionsStore,
  type IssueSidebarSection,
} from "@multica/core/issues/stores";
import { useT } from "../../i18n";
import { IssueSidebarAddMenu, IssueSidebarAddMenuProvider } from "./issue-sidebar-add-menu";
import { IssueSidebarCard } from "./issue-sidebar-section";

const CARD_GROUPS = ISSUE_SIDEBAR_GROUPS.slice(0, -1);
const FOOTER_GROUP = ISSUE_SIDEBAR_GROUPS[ISSUE_SIDEBAR_GROUPS.length - 1]!;

export function useIssueSidebarSectionLabels(): Record<IssueSidebarSection, string> {
  const { t } = useT("issues");
  return {
    properties: t(($) => $.detail.section_properties),
    quick_actions: t(($) => $.detail.section_quick_actions),
    wakeups: t(($) => $.wakeups.title),
    plugins: t(($) => $.plugins.panels),
    parent: t(($) => $.detail.section_parent_issue),
    sub_issues: t(($) => $.detail.sub_issues_label),
    duplicates: t(($) => $.duplicates.section_title),
    pull_requests: t(($) => $.detail.section_pull_requests),
    environments: t(($) => $.environments.section_title),
    deliverables: t(($) => $.deliverables.section_title),
    apps: t(($) => $.app_calls.section_title),
    execution_log: t(($) => $.execution_log.section),
    details: t(($) => $.detail.section_details),
    metadata: t(($) => $.detail.section_metadata),
  };
}

/**
 * The issue sidebar: a strip level with the page header holding the "+" menu
 * and settings, the overview card for what the issue needs next, then one card
 * for everything else, its groups of related sections split by hairlines,
 * minus the sections the reader hid. Each section sits in its own slot, which
 * collapses while the section renders nothing; a group whose slots all
 * collapse goes with them.
 */
export function IssueSidebarSections({
  sections,
  available,
  overview,
  revealed = [],
  onRevealEnd,
}: {
  sections: Record<IssueSidebarSection, ReactNode>;
  available: readonly IssueSidebarSection[];
  /** The first card: what state the issue is in, who has it, what it needs next. */
  overview?: ReactNode;
  /**
   * Hidden sections to show anyway, such as the one a header chip jumped to.
   * Switching one of them in settings ends its reveal, so the switch wins.
   */
  revealed?: readonly IssueSidebarSection[];
  onRevealEnd?: (section: IssueSidebarSection) => void;
}) {
  const { t } = useT("issues");
  const hidden = useIssueSidebarSectionsStore((s) => s.hidden);
  const containerRef = useRef<HTMLDivElement>(null);

  const isShown = (id: IssueSidebarSection) => available.includes(id) && (!hidden.includes(id) || revealed.includes(id));
  const hiddenCount = available.filter((id) => !isShown(id)).length;
  const onShownChange = (id: IssueSidebarSection) => {
    if (revealed.includes(id)) onRevealEnd?.(id);
  };
  const getEmptySections = () =>
    new Set(
      available.filter(
        (id) => isShown(id) && containerRef.current?.querySelector(`[data-issue-sidebar-section="${id}"]`)?.childElementCount === 0,
      ),
    );
  const slots = (group: readonly IssueSidebarSection[]) =>
    group.filter(isShown).map((id) => (
      <div key={id} data-issue-sidebar-section={id} className="empty:hidden">
        {sections[id]}
      </div>
    ));

  return (
    <IssueSidebarAddMenuProvider>
      <div ref={containerRef} data-mc="issue-properties">
        <div className="sticky top-0 z-10 flex h-12 items-center justify-end gap-1 border-b bg-background px-3">
          <IssueSidebarAddMenu />
          <IssueSidebarSettings
            available={available}
            getEmptySections={getEmptySections}
            onShownChange={onShownChange}
            trigger={
              <IconButton
                variant="ghost"
                size="icon-xs"
                label={t(($) => $.detail.sidebar_settings.open)}
                className="text-muted-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
              >
                <SlidersHorizontal />
              </IconButton>
            }
          />
        </div>
        <div className="flex flex-col gap-2 p-2">
          {overview && <IssueSidebarCard>{overview}</IssueSidebarCard>}
          {/* Each group draws the hairline above it; the card clips the
              first visible group's, wherever the groups before it collapsed. */}
          <IssueSidebarCard className="gap-0 overflow-clip py-0">
            <div className="-mt-px">
              {CARD_GROUPS.map((group) => {
                const shown = slots(group);
                return shown.length > 0 && (
                  <div
                    key={group[0]}
                    data-slot="issue-sidebar-group"
                    className="flex flex-col gap-3 border-t py-1.5 [&>*]:flow-root [&:not(:has(>:not(:empty)))]:hidden"
                  >
                    {shown}
                  </div>
                );
              })}
            </div>
          </IssueSidebarCard>
          <div className="flex flex-col gap-2 pt-1 [&>*]:flow-root">{slots(FOOTER_GROUP)}</div>
          {hiddenCount > 0 && (
            <IssueSidebarSettings
              available={available}
              getEmptySections={getEmptySections}
              onShownChange={onShownChange}
              side="top"
              align="start"
              trigger={
                <button
                  type="button"
                  className="flex h-7 max-w-full items-center gap-1.5 self-start rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-accent/50"
                >
                  <EyeOff className="size-3.5 shrink-0" />
                  <span className="truncate">{t(($) => $.detail.sidebar_settings.hidden_count, { count: hiddenCount })}</span>
                  <span aria-hidden="true" className="text-faint-foreground">·</span>
                  <span className="shrink-0 font-medium text-foreground">{t(($) => $.detail.sidebar_settings.customize)}</span>
                </button>
              }
            />
          )}
        </div>
      </div>
    </IssueSidebarAddMenuProvider>
  );
}

/**
 * Chooses which sections the issue sidebar shows, listed by the card they sit
 * in. Changes apply as they are made and hold for every issue on this device.
 */
export function IssueSidebarSettings({
  available,
  trigger,
  getEmptySections,
  onShownChange,
  side = "bottom",
  align = "end",
}: {
  /** Sections this sidebar can show at all; the rest stay out of the list. */
  available: readonly IssueSidebarSection[];
  trigger: ReactElement;
  /** Shown sections with nothing to render on this issue, read when the popover opens. */
  getEmptySections?: () => ReadonlySet<IssueSidebarSection>;
  onShownChange?: (section: IssueSidebarSection, shown: boolean) => void;
  side?: "top" | "bottom";
  align?: "start" | "end";
}) {
  const { t } = useT("issues");
  const labels = useIssueSidebarSectionLabels();
  const hidden = useIssueSidebarSectionsStore((s) => s.hidden);
  const customized = useIssueSidebarSectionsStore(selectIssueSidebarCustomized);
  const setHidden = useIssueSidebarSectionsStore((s) => s.setHidden);
  const reset = useIssueSidebarSectionsStore((s) => s.reset);
  const [empty, setEmpty] = useState<ReadonlySet<IssueSidebarSection>>(new Set());

  const groups = ISSUE_SIDEBAR_GROUPS.map((group) => group.filter((id) => available.includes(id))).filter((group) => group.length > 0);

  return (
    <Popover onOpenChange={(open) => open && setEmpty(getEmptySections?.() ?? new Set())}>
      <PopoverTrigger render={trigger} />
      <PopoverContent side={side} align={align} className="max-h-(--available-height) w-72 gap-0 overflow-hidden p-1">
        <div className="flex shrink-0 items-center gap-2 py-1 pr-1 pl-2">
          <span className="flex-1 text-label font-medium">{t(($) => $.detail.sidebar_settings.title)}</span>
          <button
            type="button"
            disabled={!customized}
            onClick={reset}
            className="inline-flex h-6 items-center gap-1 rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
          >
            <RotateCcw className="size-3" />
            {t(($) => $.detail.sidebar_settings.reset)}
          </button>
        </div>
        <ul className="flex min-h-0 flex-col overflow-y-auto">
          {groups.map((group, i) => (
            <Fragment key={group[0]}>
              {i > 0 && <li role="separator" className="mx-2 my-1 h-px shrink-0 bg-border" />}
              {group.map((id) => {
                const locked = ISSUE_SIDEBAR_ALWAYS_SHOWN.includes(id);
                const shown = !hidden.includes(id);
                const note = locked
                  ? t(($) => $.detail.sidebar_settings.always_shown)
                  : shown && empty.has(id)
                    ? t(($) => $.detail.sidebar_settings.empty)
                    : null;
                return (
                  <li key={id} className="flex h-8 shrink-0 items-center gap-1.5 rounded-md px-2 text-label hover:bg-accent/50">
                    <span className={cn("min-w-0 flex-1 truncate", !shown && "text-muted-foreground")}>{labels[id]}</span>
                    {note && <span className="shrink-0 text-micro text-faint-foreground">{note}</span>}
                    <Switch
                      size="sm"
                      checked={shown}
                      disabled={locked}
                      aria-label={labels[id]}
                      onCheckedChange={(next) => {
                        setHidden(id, !next);
                        onShownChange?.(id, next);
                      }}
                    />
                  </li>
                );
              })}
            </Fragment>
          ))}
        </ul>
        <p className="-mx-1 mt-1 shrink-0 border-t px-3 pt-2 pb-1.5 text-micro text-muted-foreground">
          {t(($) => $.detail.sidebar_settings.footer)}
        </p>
      </PopoverContent>
    </Popover>
  );
}
