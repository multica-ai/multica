"use client";

import { useRef, useState, type KeyboardEvent, type ReactElement, type ReactNode } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { restrictToParentElement, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { EyeOff, GripVertical, RotateCcw, SlidersHorizontal } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import {
  ISSUE_SIDEBAR_ALWAYS_SHOWN,
  selectIssueSidebarCustomized,
  selectIssueSidebarOrder,
  useIssueSidebarSectionsStore,
  type IssueSidebarSection,
} from "@multica/core/issues/stores";
import { useT } from "../../i18n";
import { IssueSidebarStack } from "./issue-sidebar-section";

const DRAG_MODIFIERS = [restrictToVerticalAxis, restrictToParentElement];

/**
 * Keyboard reordering listens for arrow keys on the document, but a Base UI
 * dialog (the mobile sidebar sheet) stops them from leaving it. Pass this as
 * the dialog popup's onKeyDown to let them through while a row is held.
 */
export function letSidebarSettingsDragKeysThrough(event: KeyboardEvent & { preventBaseUIHandler: () => void }) {
  if (event.target instanceof Element && event.target.closest('[data-sidebar-settings-handle][aria-pressed="true"]')) {
    event.preventBaseUIHandler();
  }
}

export function useIssueSidebarSectionLabels(): Record<IssueSidebarSection, string> {
  const { t } = useT("issues");
  return {
    properties: t(($) => $.detail.section_properties),
    quick_actions: t(($) => $.detail.section_quick_actions),
    wakeups: t(($) => $.wakeups.title),
    plugins: t(($) => $.plugins.panels),
    parent: t(($) => $.detail.section_parent_issue),
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
 * The issue sidebar's sections in the reader's order, minus the ones they
 * hid. Each section sits in its own slot, which collapses while the section
 * renders nothing; that is how settings tells an empty section from a shown one.
 */
export function IssueSidebarSections({
  sections,
  available,
  revealed = [],
  onRevealEnd,
}: {
  sections: Record<IssueSidebarSection, ReactNode>;
  available: readonly IssueSidebarSection[];
  /**
   * Hidden sections to show anyway, such as the one a header chip jumped to.
   * Switching one of them in settings ends its reveal, so the switch wins.
   */
  revealed?: readonly IssueSidebarSection[];
  onRevealEnd?: (section: IssueSidebarSection) => void;
}) {
  const { t } = useT("issues");
  const order = useIssueSidebarSectionsStore(selectIssueSidebarOrder);
  const hidden = useIssueSidebarSectionsStore((s) => s.hidden);
  const containerRef = useRef<HTMLDivElement>(null);

  const isShown = (id: IssueSidebarSection) => !hidden.includes(id) || revealed.includes(id);
  const visible = order.filter((id) => available.includes(id) && isShown(id));
  const hiddenCount = available.filter((id) => !isShown(id)).length;
  const onShownChange = (id: IssueSidebarSection) => {
    if (revealed.includes(id)) onRevealEnd?.(id);
  };
  const getEmptySections = () =>
    new Set(
      visible.filter(
        (id) => containerRef.current?.querySelector(`[data-issue-sidebar-section="${id}"]`)?.childElementCount === 0,
      ),
    );

  return (
    <div ref={containerRef} className="relative">
      <IssueSidebarSettings
        available={available}
        getEmptySections={getEmptySections}
        onShownChange={onShownChange}
        trigger={
          <IconButton
            variant="ghost"
            size="icon-xs"
            label={t(($) => $.detail.sidebar_settings.open)}
            className="absolute top-0.5 right-2 z-10 text-muted-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
          >
            <SlidersHorizontal />
          </IconButton>
        }
      />
      {/* The first header to render shares its row with the settings button. */}
      <IssueSidebarStack className="[&>:nth-child(1_of_:not(:empty))>*>[data-slot=issue-sidebar-section-header]]:pr-9">
        {visible.map((id) => (
          <div key={id} data-issue-sidebar-section={id} className="empty:hidden">
            {sections[id]}
          </div>
        ))}
        {hiddenCount > 0 && (
          <div className="px-2">
            <IssueSidebarSettings
              available={available}
              getEmptySections={getEmptySections}
              onShownChange={onShownChange}
              side="top"
              align="start"
              trigger={
                <button
                  type="button"
                  className="flex h-7 max-w-full items-center gap-1.5 rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-accent/50"
                >
                  <EyeOff className="size-3.5 shrink-0" />
                  <span className="truncate">{t(($) => $.detail.sidebar_settings.hidden_count, { count: hiddenCount })}</span>
                  <span aria-hidden="true" className="text-faint-foreground">·</span>
                  <span className="shrink-0 font-medium text-foreground">{t(($) => $.detail.sidebar_settings.customize)}</span>
                </button>
              }
            />
          </div>
        )}
      </IssueSidebarStack>
    </div>
  );
}

/**
 * Chooses which sections the issue sidebar shows and in what order. Changes
 * apply as they are made and hold for every issue on this device.
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
  const fullOrder = useIssueSidebarSectionsStore(selectIssueSidebarOrder);
  const hidden = useIssueSidebarSectionsStore((s) => s.hidden);
  const customized = useIssueSidebarSectionsStore(selectIssueSidebarCustomized);
  const setOrder = useIssueSidebarSectionsStore((s) => s.setOrder);
  const setHidden = useIssueSidebarSectionsStore((s) => s.setHidden);
  const reset = useIssueSidebarSectionsStore((s) => s.reset);
  const [empty, setEmpty] = useState<ReadonlySet<IssueSidebarSection>>(new Set());
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const order = fullOrder.filter((id) => available.includes(id));

  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const from = fullOrder.indexOf(active.id as IssueSidebarSection);
    const to = fullOrder.indexOf(over.id as IssueSidebarSection);
    setOrder(arrayMove([...fullOrder], from, to));
  };

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
        <DndContext sensors={sensors} collisionDetection={closestCenter} modifiers={DRAG_MODIFIERS} onDragEnd={onDragEnd}>
          <SortableContext items={order} strategy={verticalListSortingStrategy}>
            <ul className="relative flex min-h-0 flex-col overflow-y-auto">
              {order.map((id) => {
                const locked = ISSUE_SIDEBAR_ALWAYS_SHOWN.includes(id);
                const shown = !hidden.includes(id);
                const note = locked
                  ? t(($) => $.detail.sidebar_settings.always_shown)
                  : shown && empty.has(id)
                    ? t(($) => $.detail.sidebar_settings.empty)
                    : null;
                return (
                  <SectionRow
                    key={id}
                    id={id}
                    label={labels[id]}
                    note={note}
                    shown={shown}
                    locked={locked}
                    onShownChange={(next) => {
                      setHidden(id, !next);
                      onShownChange?.(id, next);
                    }}
                  />
                );
              })}
            </ul>
          </SortableContext>
        </DndContext>
        <p className="-mx-1 mt-1 shrink-0 border-t px-3 pt-2 pb-1.5 text-micro text-muted-foreground">
          {t(($) => $.detail.sidebar_settings.footer)}
        </p>
      </PopoverContent>
    </Popover>
  );
}

function SectionRow({
  id,
  label,
  note,
  shown,
  locked,
  onShownChange,
}: {
  id: IssueSidebarSection;
  label: string;
  note: string | null;
  shown: boolean;
  locked: boolean;
  onShownChange: (shown: boolean) => void;
}) {
  const { t } = useT("issues");
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id });
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        "flex h-8 shrink-0 items-center gap-1.5 rounded-md pr-2 pl-1 text-label",
        isDragging ? "relative z-10 bg-popover shadow-[var(--menu-shadow)] ring-1 ring-surface-border" : "hover:bg-accent/50",
      )}
    >
      <button
        ref={setActivatorNodeRef}
        type="button"
        {...attributes}
        {...listeners}
        data-sidebar-settings-handle=""
        aria-label={t(($) => $.detail.sidebar_settings.drag_handle, { section: label })}
        className="flex size-5 shrink-0 cursor-grab touch-none items-center justify-center rounded-sm text-faint-foreground outline-none hover:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing"
      >
        <GripVertical className="size-3.5" />
      </button>
      <span className={cn("min-w-0 flex-1 truncate", !shown && "text-muted-foreground")}>{label}</span>
      {note && <span className="shrink-0 text-micro text-faint-foreground">{note}</span>}
      <Switch size="sm" checked={shown} disabled={locked} onCheckedChange={onShownChange} aria-label={label} />
    </li>
  );
}
