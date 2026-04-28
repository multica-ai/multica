"use client";

import { useQuery } from "@tanstack/react-query";
import { Globe, Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useModalStore } from "@multica/core/modals";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { workspaceColor } from "@multica/core/workspace/color";
import type { Workspace } from "@multica/core/types";
import { AppLink, useNavigation } from "../navigation";

export const WORKSPACE_RAIL_WIDTH_PX = 56;

/**
 * Persistent left sidebar shown on every authenticated dashboard route.
 * Top → "All workspaces" (cross-workspace meta view). Middle → one
 * tile per workspace the user belongs to. Bottom → "New workspace"
 * trigger that opens the existing create-workspace modal.
 *
 * Active state rules:
 *   - The "All workspaces" tile is active iff the pathname starts with
 *     `/global` (the prefix is reserved — see paths/reserved-slugs.ts).
 *   - A workspace tile is active iff the first path segment equals that
 *     workspace's slug.
 *
 * The component reads `listWorkspaces()` directly. It MUST work outside
 * `WorkspaceSlugProvider` because `/global` has no current workspace.
 */
export function WorkspaceRail() {
  const { pathname } = useNavigation();
  const { data: workspaces, isPending } = useQuery(workspaceListOptions());

  const firstSegment = pathname.split("/").filter(Boolean)[0] ?? "";
  const isGlobalActive = firstSegment === "global";
  const openCreateWorkspaceModal = () =>
    useModalStore.getState().open("create-workspace");

  return (
    <nav
      aria-label="Workspaces"
      // `relative z-20` keeps the rail above the AppSidebar (z-10) during the
      // sidebar's open/close slide transition; otherwise the fixed sidebar
      // briefly paints over the rail's icons mid-animation.
      className="relative z-20 flex h-svh shrink-0 flex-col items-center gap-1 border-r border-border bg-sidebar py-2"
      style={{ width: WORKSPACE_RAIL_WIDTH_PX }}
    >
      <RailLink
        href={paths.global()}
        label="All workspaces"
        active={isGlobalActive}
      >
        <Globe className="size-4" aria-hidden="true" />
      </RailLink>

      <div className="my-1 h-px w-6 bg-border" aria-hidden="true" />

      <ul className="flex w-full flex-1 flex-col items-center gap-1 overflow-y-auto px-1">
        {isPending ? (
          <RailSkeletons />
        ) : (
          workspaces?.map((ws) => (
            <li key={ws.id} className="flex w-full justify-center">
              <RailLink
                href={paths.workspace(ws.slug).issues()}
                label={ws.name}
                active={firstSegment === ws.slug}
              >
                <WorkspaceTile workspace={ws} />
              </RailLink>
            </li>
          ))
        )}
      </ul>

      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={openCreateWorkspaceModal}
              aria-label="Create workspace"
              className={cn(
                "flex size-9 items-center justify-center rounded-md border border-transparent text-muted-foreground transition-colors",
                "hover:border-border hover:bg-sidebar-accent hover:text-sidebar-accent-foreground",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              )}
            />
          }
        >
          <Plus className="size-4" aria-hidden="true" />
        </TooltipTrigger>
        <TooltipContent side="right" sideOffset={6}>
          New workspace
        </TooltipContent>
      </Tooltip>
    </nav>
  );
}

interface RailLinkProps {
  href: string;
  label: string;
  active: boolean;
  children: React.ReactNode;
}

function RailLink({ href, label, active, children }: RailLinkProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <AppLink
            href={href}
            aria-label={label}
            aria-current={active ? "page" : undefined}
            className={cn(
              "group relative flex size-9 items-center justify-center rounded-md transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              active
                ? "bg-sidebar-accent text-sidebar-accent-foreground"
                : "text-muted-foreground hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground",
            )}
          >
            {/* Active indicator — vertical bar flush with the rail's left edge,
                Linear-style. Inset 6px from the tile so it sits in the gutter. */}
            <span
              aria-hidden="true"
              className={cn(
                "absolute -left-[10px] top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-r-full bg-foreground transition-opacity",
                active ? "opacity-100" : "opacity-0 group-hover:opacity-40",
              )}
            />
            {children}
          </AppLink>
        }
      />
      <TooltipContent side="right" sideOffset={6}>
        {label}
      </TooltipContent>
    </Tooltip>
  );
}

function WorkspaceTile({ workspace }: { workspace: Workspace }) {
  const initial = (workspace.name.trim().charAt(0) || "W").toUpperCase();
  const bg = workspaceColor(workspace.id);
  return (
    <span
      className="flex size-7 items-center justify-center rounded-md text-[11px] font-semibold text-white"
      style={{ backgroundColor: bg }}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

function RailSkeletons() {
  // Three placeholder tiles — matches the median workspace count for our
  // self-host users, so the rail width / vertical rhythm doesn't shift
  // when listWorkspaces() resolves.
  return (
    <>
      {[0, 1, 2].map((i) => (
        <li
          key={i}
          aria-hidden="true"
          className="flex w-full justify-center"
        >
          <div className="size-7 animate-pulse rounded-md bg-sidebar-accent/60" />
        </li>
      ))}
    </>
  );
}
