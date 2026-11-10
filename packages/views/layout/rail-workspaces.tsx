"use client";

import { useMemo, useState, type MouseEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import { AppWindow, ArrowBigUp, Check, Mail, Pin, Plus } from "@multica/ui/icons";
import type { api } from "@multica/core/api";
import { useConfigStore } from "@multica/core/config";
import {
  inboxUnreadSummaryEnabled,
  inboxUnreadSummaryOptions,
  unreadWorkspaceIds,
} from "@multica/core/inbox/queries";
import { paths, useCurrentWorkspace } from "@multica/core/paths";
import type { Invitation, Workspace } from "@multica/core/types";
import {
  myInvitationListOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { useSetWorkspacePinned } from "@multica/core/workspace/mutations";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Kbd, KbdGroup } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";

// Stable empty defaults: an inline `= []` is a new array every render.
const EMPTY_WORKSPACES: Workspace[] = [];
const EMPTY_INVITATIONS: Invitation[] = [];
const EMPTY_INBOX_SUMMARY: Awaited<ReturnType<typeof api.getInboxUnreadSummary>> = [];
const EMPTY_SLUGS: ReadonlySet<string> = new Set();

function UnreadDot({ className }: { className?: string }) {
  return <span aria-hidden className={cn("size-2 shrink-0 rounded-full bg-brand", className)} />;
}

// Shift alone, the browser's "open in new window" gesture. Cmd/Ctrl+Shift
// stays the foreground-tab gesture AppLink already handles.
function isNewWindowClick(event: MouseEvent) {
  return event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey;
}

/**
 * Trailing row button that opens the workspace in a new window. It sits
 * inside the row link, so its click cancels the link's navigation and still
 * reaches the menu item, which closes the menu.
 */
function NewWindowButton({ onOpen }: { onOpen: () => void }) {
  const { t } = useT("layout");
  const label = t(($) => $.rail.open_in_new_window);
  return (
    <TooltipHint
      label={label}
      side="right"
      shortcut={
        <KbdGroup>
          <Kbd>
            <ArrowBigUp />
          </Kbd>
          <Kbd>{t(($) => $.rail.open_in_new_window_click)}</Kbd>
        </KbdGroup>
      }
    >
      <button
        type="button"
        tabIndex={-1}
        aria-label={label}
        className="-my-1 -mr-1 inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground opacity-0 group-hover/workspace:opacity-100 group-data-highlighted/workspace:opacity-100 hover:bg-surface-selected hover:text-foreground"
        onClick={(event) => {
          event.preventDefault();
          onOpen();
        }}
      >
        <AppWindow />
      </button>
    </TooltipHint>
  );
}

/**
 * Pin toggle that takes the trailing status slot on hover/highlight. Its tone
 * follows the row's unread state, so pinning never hides the unread signal.
 */
function PinButton({ pinned, unread, onToggle }: { pinned: boolean; unread: boolean; onToggle: () => void }) {
  const { t } = useT("layout");
  const label = pinned ? t(($) => $.rail.unpin_workspace) : t(($) => $.rail.pin_workspace);
  return (
    <TooltipHint label={label} side="right">
      <button type="button" aria-label={label} tabIndex={0}
        className={cn("absolute inset-0 flex items-center justify-center rounded-md opacity-0 group-hover/workspace:opacity-100 group-data-highlighted/workspace:opacity-100 focus-visible:opacity-100 hover:bg-surface-selected", unread ? "text-brand" : "text-muted-foreground")}
        onClick={(event) => { event.preventDefault(); event.stopPropagation(); onToggle(); }}
        onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") event.stopPropagation(); }}
      >
        <Pin className={cn("size-4", pinned && "fill-current")} />
      </button>
    </TooltipHint>
  );
}

// A text tag rather than a window glyph, so it never reads as the row
// button that opens a new window.
function OtherWindowMarker() {
  const { t } = useT("layout");
  return (
    <TooltipHint label={t(($) => $.rail.open_in_other_window)} side="right">
      <span className="shrink-0 text-caption text-muted-foreground">
        {t(($) => $.rail.in_other_window)}
      </span>
    </TooltipHint>
  );
}

/**
 * The workspace switcher at the top of the rail: one tile for the current
 * workspace, whatever the workspace count, so the spaces below never move.
 * Its menu lists every workspace, an entry to the invitations page while
 * any are pending, and workspace creation. The tile carries a dot while another workspace has unread inbox
 * items or an invitation waits; the current workspace's unread shows on the
 * Inbox space instead.
 */
export function RailWorkspaces() {
  const { t } = useT("layout");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id;
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());
  const { data: invitations = EMPTY_INVITATIONS } = useQuery(myInvitationListOptions());
  // One cross-workspace cache entry; gated on an active workspace since the
  // endpoint resolves through the workspace-member middleware.
  const { data: unreadSummary = EMPTY_INBOX_SUMMARY } = useQuery({
    ...inboxUnreadSummaryOptions(),
    enabled: inboxUnreadSummaryEnabled(wsId),
  });
  const unreadIds = useMemo(() => unreadWorkspaceIds(unreadSummary), [unreadSummary]);
  const setPinned = useSetWorkspacePinned();
  const { openWorkspaceInNewWindow, workspacesInOtherWindows } = useNavigation();
  // Read when the menu opens: other windows switch workspaces independently.
  const [inOtherWindows, setInOtherWindows] = useState(EMPTY_SLUGS);

  const current = workspaces.find((ws) => ws.id === wsId) ?? workspace;
  if (!current) return null;
  const otherUnread = workspaces.some((ws) => ws.id !== wsId && unreadIds.has(ws.id));
  const label = t(($) => $.rail.switch_workspace);

  return (
    <DropdownMenu
      onOpenChange={(open) => {
        if (open && workspacesInOtherWindows) {
          setInOtherWindows(new Set(workspacesInOtherWindows()));
        }
      }}
    >
      <TooltipHint label={current.name} side="right">
        <DropdownMenuTrigger
          aria-label={label}
          className="relative inline-flex size-[34px] shrink-0 cursor-pointer items-center justify-center rounded-lg outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <WorkspaceAvatar
            name={current.name}
            avatarUrl={current.avatar_url}
            className="h-[34px] w-[34px] rounded-lg border-surface-border bg-surface-raised text-body text-foreground shadow-[var(--surface-shadow)]"
          />
          {(otherUnread || invitations.length > 0) && (
            <UnreadDot className="absolute -top-0.5 -right-0.5 ring-2 ring-app-shell" />
          )}
        </DropdownMenuTrigger>
      </TooltipHint>
      <DropdownMenuContent side="right" align="start" sideOffset={10} className="w-64">
        <DropdownMenuGroup className="max-h-80 overflow-y-auto">
          <DropdownMenuLabel className="text-caption text-muted-foreground">
            {t(($) => $.sidebar.workspaces_label)}
          </DropdownMenuLabel>
          {workspaces.map((ws) => {
            const isCurrent = ws.id === wsId;
            // A workspace shown by another window is focused there by a
            // plain click, so it offers no new window of its own.
            const inOtherWindow = !isCurrent && inOtherWindows.has(ws.slug);
            const openInNewWindow =
              openWorkspaceInNewWindow && !isCurrent && !inOtherWindow
                ? () => openWorkspaceInNewWindow(ws.slug)
                : null;
            return (
              <DropdownMenuItem
                key={ws.id}
                aria-current={isCurrent ? "page" : undefined}
                className="group/workspace"
                onClick={
                  openInNewWindow
                    ? (event: MouseEvent) => {
                        if (event.defaultPrevented || !isNewWindowClick(event)) return;
                        event.preventDefault();
                        openInNewWindow();
                      }
                    : undefined
                }
                render={<AppLink href={paths.workspace(ws.slug).issues()} />}
              >
                <WorkspaceAvatar name={ws.name} avatarUrl={ws.avatar_url} size="sm" />
                <span className="min-w-0 flex-1 truncate">{ws.name}</span>
                {isCurrent && <Check />}
                <span className="relative flex size-5 shrink-0 items-center justify-center">
                  <span className="flex group-hover/workspace:opacity-0 group-data-highlighted/workspace:opacity-0">
                    {ws.pinned_at ? (
                      <Pin className={cn("size-4 fill-current", !isCurrent && unreadIds.has(ws.id) ? "text-brand" : "text-muted-foreground")} />
                    ) : !isCurrent && unreadIds.has(ws.id) ? <UnreadDot /> : null}
                  </span>
                  <PinButton pinned={!!ws.pinned_at} unread={!isCurrent && unreadIds.has(ws.id)} onToggle={() => setPinned.mutate({ workspaceId: ws.id, pinned: !ws.pinned_at })} />
                </span>
                {inOtherWindow && <OtherWindowMarker />}
                {openInNewWindow && <NewWindowButton onOpen={openInNewWindow} />}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuGroup>
        {invitations.length > 0 && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem render={<AppLink href={paths.invitations()} />}>
                <Mail />
                <span className="min-w-0 flex-1 truncate">
                  {t(($) => $.sidebar.pending_invitations_label)}
                </span>
                <span className="text-caption text-muted-foreground tabular-nums">
                  {invitations.length}
                </span>
                <UnreadDot />
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </>
        )}
        {!workspaceCreationDisabled && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem render={<AppLink href={paths.newWorkspace()} />}>
                <Plus />
                {t(($) => $.sidebar.create_workspace)}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
