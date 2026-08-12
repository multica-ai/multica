"use client";

import { cn } from "@multica/ui/lib/utils";
import { SidebarTrigger, useSidebarSafe } from "@multica/ui/components/ui/sidebar";

/**
 * The way back to the nav wherever it is not a permanent column: a sheet below
 * the compact breakpoint, auto-collapsed from there up to `xl`.
 *
 * Every surface below `xl` needs one of these. `PageHeader` supplies it for
 * free, so this is exported for the pages that build their own chrome instead
 * — without it a touch user has no way to reopen the nav at all. Renders
 * nothing outside a `SidebarProvider` so such a page still stands alone.
 */
export function CollapsedNavTrigger() {
  const sidebar = useSidebarSafe();
  if (!sidebar) return null;
  return <SidebarTrigger className="mr-2 xl:hidden" />;
}

interface PageHeaderProps {
  children: React.ReactNode;
  /**
   * Replaces the mobile sidebar trigger at the far left.
   *
   * For a surface a phone reaches by drilling in rather than by navigating —
   * the inbox's issue detail — "go back" is the leading affordance that
   * matters, and the sidebar is still one step away behind it. Rendering both
   * would spend two of the header's 48px on navigation chrome and leave the
   * title nothing to truncate into.
   */
  leading?: React.ReactNode;
  className?: string;
}

export function PageHeader({ children, leading, className }: PageHeaderProps) {
  return (
    <header className={cn("flex h-12 shrink-0 items-center border-b px-4", className)}>
      {leading ?? <CollapsedNavTrigger />}
      {children}
    </header>
  );
}
