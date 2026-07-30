"use client";

import type { ReactNode } from "react";
import { SidebarTrigger, useSidebarSafe } from "@multica/ui/components/ui/sidebar";

/**
 * The frame every operating-system page sits in (FIR-3589 items 5 and 7).
 *
 * Two things every other page in the product already has, and these four did
 * not: the product page header — which also carries the mobile sidebar
 * trigger, so a phone can reach the navigation at all — and a body that owns
 * its own scrolling. Without the second one a tall page (the role chart above
 * all) simply ran off the bottom of the viewport with no way to reach it.
 *
 * It matches @multica/views' PageHeader rather than importing it:
 * @multica/views already depends on this package for the sidebar's
 * operating-system links, so depending back on it would be a package cycle.
 */
export function OsPageShell({ title, subtitle, headerActions, children }: {
  title: string;
  subtitle?: string;
  headerActions?: ReactNode;
  children: ReactNode;
}) {
  const sidebar = useSidebarSafe();
  return (
    <div className="flex h-full min-w-0 flex-col">
      <div className="sticky top-0 z-10 flex h-12 shrink-0 items-center justify-between gap-3 border-b bg-background px-2 sm:px-4">
        <div className="flex min-w-0 items-center">
          {sidebar && <SidebarTrigger className="mr-2 md:hidden" />}
          <div className="flex min-w-0 flex-col">
            <h1 className="truncate text-sm font-semibold">{title}</h1>
            {subtitle && <p className="truncate text-[11px] text-muted-foreground">{subtitle}</p>}
          </div>
        </div>
        {headerActions && <div className="flex shrink-0 items-center gap-2">{headerActions}</div>}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}
