"use client";

// CEREBRO-PATCH(page-header-cerebro): cerebro modification of upstream file

import { cn } from "@multica/ui/lib/utils";
import { SidebarTrigger, useSidebar } from "@multica/ui/components/ui/sidebar";

export function MobileSidebarTrigger({ className }: { className?: string }) {
  try {
    useSidebar();
  } catch {
    return null;
  }
  return <SidebarTrigger className={cn("mr-2 md:hidden", className)} />;
}

interface PageHeaderProps {
  children: React.ReactNode;
  className?: string;
}

export function PageHeader({ children, className }: PageHeaderProps) {
  return (
    <div
      className={cn(
        // CEREBRO-PATCH(page-header-overflow-scroll): JEH-1144 — allow horizontal scroll when header content (title + actions) exceeds viewport on mobile; no-scrollbar hides the bar so it doesn't take vertical space inside the 12-unit header.
        "sticky top-0 z-10 flex h-12 shrink-0 items-center border-b bg-background px-2 sm:px-4 overflow-x-auto no-scrollbar",
        className,
      )}
    >
      {/* CEREBRO-PATCH(page-header-sticky): keep page headers pinned during mobile keyboard viewport changes. */}
      <MobileSidebarTrigger />
      {children}
    </div>
  );
}
