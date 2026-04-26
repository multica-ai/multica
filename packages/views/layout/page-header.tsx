"use client";

import { cn } from "@multica/ui/lib/utils";
import { SidebarTrigger, useSidebar } from "@multica/ui/components/ui/sidebar";

function MobileSidebarTrigger() {
  try {
    useSidebar();
  } catch {
    return null;
  }
  return <SidebarTrigger className="mr-2 md:hidden" />;
}

interface PageHeaderProps {
  children: React.ReactNode;
  className?: string;
}

export function PageHeader({ children, className }: PageHeaderProps) {
  return (
    <div className={cn("flex shrink-0 items-center border-b px-4 py-2", className)}>
      <MobileSidebarTrigger />
      <div className="flex flex-col justify-center min-h-[2.5rem] w-full">
        {children}
      </div>
    </div>
  );
}
