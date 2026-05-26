"use client";

import { cn } from "@multica/ui/lib/utils";
import { SidebarTrigger, useSidebar } from "@multica/ui/components/ui/sidebar";

function SidebarToggle() {
  try {
    const { state } = useSidebar();
    // On mobile: always show (hamburger). On desktop: only when sidebar is collapsed.
    return <SidebarTrigger className={cn("mr-2", state === "expanded" && "md:hidden")} />;
  } catch {
    return null;
  }
}

interface PageHeaderProps {
  children: React.ReactNode;
  className?: string;
}

export function PageHeader({ children, className }: PageHeaderProps) {
  return (
    <div className={cn("flex h-12 shrink-0 items-center border-b px-4", className)}>
      <SidebarToggle />
      {children}
    </div>
  );
}
