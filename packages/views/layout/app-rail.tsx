"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Settings, Users, type IconComponent } from "@multica/ui/icons";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { countUnreadChatMessages } from "@multica/core/chat/unread";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { orderedRailSpaces, useRailOrderStore } from "@multica/core/navigation";
import {
  useCurrentWorkspace,
  useWorkspacePaths,
  type AppSpaceId,
  type RailSpaceId,
} from "@multica/core/paths";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { CappedNumberFlow } from "@multica/ui/components/ui/number-flow";
import { cn } from "@multica/ui/lib/utils";
import { useAppForeground } from "../common/use-app-foreground";
import { useT } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { AccountMenu } from "./account-menu";
import { useAppShell } from "./app-shell-context";
import { NewIssueButton, SearchButton } from "./global-actions";
import { HelpLauncher } from "./help-launcher";
import { isPathWithin } from "./panel-nav";
import { useTeamAgentStatus } from "./panels/team-status";
import { RAIL_ICON_BUTTON_CLASS } from "./rail-styles";
import { RailWorkspaces } from "./rail-workspaces";
import { routeIconForPath } from "./route-icon-components";

type RailBadge =
  | { kind: "count"; value: number; tone: "brand" | "neutral" }
  | { kind: "dot" };

/**
 * Rail badges. Inbox and Chat count unread; Team only flags that something
 * needs a look — the panel says what. A dot must be something the user can
 * clear, so a failed autopilot run is shown in the Autopilots panel only.
 */
function useRailBadges(): Partial<Record<RailSpaceId, RailBadge>> {
  const wsId = useCurrentWorkspace()?.id;
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const inboxUnread = useInboxUnreadCount(wsId);

  const { data: chatSessions = [] } = useQuery({
    ...chatSessionsOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  // The session the user is reading right now must not count: its row shows 0
  // (auto mark-read is about to clear it) and a reply landing there would
  // flash a count with no matching row. "Reading" = a session is active, a
  // chat surface shows it (the chat route or the floating window), and the
  // app is in the foreground. Backgrounded, auto mark-read is suppressed
  // (MUL-4485), so the reply stays unread and must count.
  const activeChatSessionId = useChatStore((s) => s.activeSessionId);
  const floatingChatOpen = useChatStore((s) => s.isOpen);
  const appForeground = useAppForeground();
  const viewedChatSessionId =
    appForeground && (floatingChatOpen || isPathWithin(pathname, p.chat()))
      ? activeChatSessionId
      : null;
  const chatUnread = useMemo(
    () => countUnreadChatMessages(chatSessions, viewedChatSessionId),
    [chatSessions, viewedChatSessionId],
  );

  const { stuck } = useTeamAgentStatus(wsId);

  const badges: Partial<Record<RailSpaceId, RailBadge>> = {};
  if (inboxUnread > 0) badges.inbox = { kind: "count", value: inboxUnread, tone: "brand" };
  if (chatUnread > 0) badges.chat = { kind: "count", value: chatUnread, tone: "neutral" };
  if (stuck.length > 0) badges.team = { kind: "dot" };
  return badges;
}

function RailBadgeMark({ badge }: { badge: RailBadge }) {
  // The ring cuts the badge out of whatever the item is painted with.
  const ring = "ring-2 ring-app-shell group-aria-[current=page]:ring-sidebar-accent";
  if (badge.kind === "dot") {
    return (
      <span
        aria-hidden
        className={cn(
          "absolute top-1.5 right-3.5 size-2 rounded-full bg-warning",
          ring,
        )}
      />
    );
  }
  return (
    <CappedNumberFlow
      value={badge.value}
      animated={false}
      className={cn(
        "absolute top-0.5 right-1.5 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none font-medium tabular-nums",
        ring,
        badge.tone === "brand" ? "bg-brand text-brand-foreground" : "bg-foreground text-background",
      )}
    />
  );
}

/** Hover previews of a collapsed nav. Mouse only: a touch tap navigates. */
function usePeekHandlers(space: AppSpaceId, peekable: boolean) {
  const shell = useAppShell();
  return {
    onPointerEnter: (e: React.PointerEvent) => {
      if (!shell || e.pointerType !== "mouse") return;
      if (peekable && shell.navCollapsed) shell.requestPeek(space);
      else if (shell.peekSpace) shell.releasePeek();
    },
    onPointerLeave: (e: React.PointerEvent) => {
      if (shell && e.pointerType === "mouse") shell.releasePeek();
    },
  };
}

function RailSpaceLink({
  space,
  href,
  icon: Icon,
  label,
  fullLabel,
  active,
  badge,
  peekable,
}: {
  space: RailSpaceId;
  href: string;
  icon: IconComponent;
  label: string;
  fullLabel: string;
  active: boolean;
  badge?: RailBadge;
  /** False while a rail drag is live, so hovering peers does not open their peek panel. */
  peekable: boolean;
}) {
  const peekHandlers = usePeekHandlers(space, peekable);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: space });

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={isDragging ? "opacity-30" : undefined}
      {...attributes}
      {...listeners}
    >
      <TooltipHint label={label === fullLabel ? null : fullLabel} side="right">
        <AppLink
          href={href}
          data-space={space}
          newTabTitle={fullLabel}
          // The sortable item owns pointer drags; a native link drag would
          // swallow them (same call as the pinned rows).
          draggable={false}
          aria-current={active ? "page" : undefined}
          {...peekHandlers}
          className="group relative flex h-[52px] w-[52px] flex-col items-center justify-center gap-1 rounded-xl text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-ring aria-[current=page]:bg-sidebar-accent aria-[current=page]:font-medium aria-[current=page]:text-sidebar-accent-foreground"
        >
          <Icon className="size-5" />
          <span className="max-w-full truncate px-1 text-micro leading-[14px]">{label}</span>
          {badge && <RailBadgeMark badge={badge} />}
        </AppLink>
      </TooltipHint>
    </li>
  );
}

/**
 * The app rail: workspaces, New issue and search, the five spaces, then help,
 * settings and the account. It sits on the app shell, outside the content
 * card, and stays put while the panel beside it collapses. Desktop passes
 * `globalActions={false}`: its title bar carries New issue and search.
 *
 * The space list is drag-reorderable (pointer drag, or Space to pick an item
 * up and the arrow keys to move it); the order persists per device.
 */
export function AppRail({
  topSlot,
  globalActions = true,
  className,
}: {
  topSlot?: React.ReactNode;
  globalActions?: boolean;
  className?: string;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const shell = useAppShell();
  const badges = useRailBadges();
  const space = shell?.space ?? null;
  const settingsPeek = usePeekHandlers("settings", space === "settings");

  const savedOrder = useRailOrderStore((s) => s.order);
  const setRailOrder = useRailOrderStore((s) => s.setRailOrder);
  const spaces = useMemo(() => orderedRailSpaces(savedOrder), [savedOrder]);
  // 5px of slack keeps taps and clicks navigational; past it, the drag owns
  // the pointer and the release must not open the item.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const [dragging, setDragging] = useState(false);
  // A finished drag releases the pointer over whatever item is under it, and
  // the browser follows with a click on that item's link. Swallow clicks in
  // the brief window after a drag ends, on capture, so a drop does not also
  // navigate — including drops that land on a different space. A timestamp
  // (not a flag) keeps keyboard-initiated drags and later clicks safe.
  const dragEndedAtRef = useRef(0);
  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setDragging(false);
      dragEndedAtRef.current = performance.now();
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const from = spaces.indexOf(active.id as RailSpaceId);
      const to = spaces.indexOf(over.id as RailSpaceId);
      if (from === -1 || to === -1) return;
      setRailOrder(arrayMove(spaces, from, to));
    },
    [setRailOrder, spaces],
  );

  const items: Record<RailSpaceId, { href: string; icon: IconComponent; label: string; fullLabel: string }> = {
    inbox: {
      href: p.inbox(),
      icon: routeIconForPath(p.inbox()),
      label: t(($) => $.rail.inbox),
      fullLabel: t(($) => $.nav.inbox),
    },
    work: {
      href: p.issues(),
      icon: routeIconForPath(p.issues()),
      label: t(($) => $.rail.work),
      fullLabel: t(($) => $.spaces.work),
    },
    team: {
      href: p.agents(),
      icon: Users,
      label: t(($) => $.rail.team),
      fullLabel: t(($) => $.spaces.team),
    },
    chat: {
      href: p.chat(),
      icon: routeIconForPath(p.chat()),
      label: t(($) => $.rail.chat),
      fullLabel: t(($) => $.nav.chat),
    },
    autopilots: {
      href: p.autopilots(),
      icon: routeIconForPath(p.autopilots()),
      label: t(($) => $.rail.autopilots),
      fullLabel: t(($) => $.nav.autopilots),
    },
  };

  return (
    <nav
      aria-label={t(($) => $.rail.label)}
      data-slot="app-rail"
      className={cn(
        "flex h-full w-16 shrink-0 flex-col items-center overflow-y-auto overflow-x-hidden pt-3 pb-2.5 [scrollbar-width:none]",
        className,
      )}
    >
      {topSlot}
      <RailWorkspaces />
      {globalActions ? (
        <div className="my-2 flex flex-col items-center gap-1">
          <NewIssueButton className={RAIL_ICON_BUTTON_CLASS} tooltipSide="right" />
          <SearchButton className={RAIL_ICON_BUTTON_CLASS} tooltipSide="right" />
        </div>
      ) : (
        <div className="h-2 shrink-0" />
      )}
      <ul
        className="flex flex-col items-center gap-0.5"
        onClickCapture={(event) => {
          if (performance.now() - dragEndedAtRef.current < 150) {
            event.preventDefault();
            event.stopPropagation();
          }
        }}
      >
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragStart={() => setDragging(true)}
          onDragEnd={handleDragEnd}
          onDragCancel={() => setDragging(false)}
        >
          <SortableContext items={spaces} strategy={verticalListSortingStrategy}>
            {spaces.map((id) => (
              <RailSpaceLink
                key={id}
                space={id}
                {...items[id]}
                href={shell?.spaceHref(id, items[id].href) ?? items[id].href}
                active={space === id}
                badge={badges[id]}
                peekable={!dragging}
              />
            ))}
          </SortableContext>
        </DndContext>
      </ul>
      <div className="mt-auto flex flex-col items-center gap-1 pt-2">
        <HelpLauncher />
        <TooltipHint label={t(($) => $.nav.settings)} side="right">
          <AppLink
            href={shell?.spaceHref("settings", p.settings()) ?? p.settings()}
            aria-label={t(($) => $.nav.settings)}
            aria-current={space === "settings" ? "page" : undefined}
            // Settings has no standalone preview; it only floats its own
            // list column while open.
            {...settingsPeek}
            className={RAIL_ICON_BUTTON_CLASS}
          >
            <Settings />
          </AppLink>
        </TooltipHint>
        <AccountMenu />
      </div>
    </nav>
  );
}
