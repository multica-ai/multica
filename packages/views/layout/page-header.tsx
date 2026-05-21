"use client";

import { Menu } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { useSidebarOptional } from "@multica/ui/components/ui/sidebar";

export function MobileSidebarTrigger() {
  const { t } = useTranslation("ui");
  const sidebar = useSidebarOptional();
  // PageHeader is rendered both inside the dashboard shell (where a
  // SidebarProvider exists) and on standalone pages (where it does not).
  // No provider → no sidebar to toggle, so we render nothing rather than
  // throw or guard with try/catch.
  if (!sidebar) return null;
  // The shared SidebarTrigger ships with PanelLeftIcon, which on mobile
  // sits next to the right-edge PanelRight (issue detail's properties
  // toggle). The two outlined-rectangle icons read as the same affordance
  // and users could not tell them apart. Inline the trigger here with a
  // proper hamburger so the left = "all the rest of the app" intent reads
  // distinctly from the right = "this issue's properties".
  // 36px tap target — closer to iOS 44pt minimum without pushing the
  // page-header taller. -ml-1.5 nudges the icon left so its visible
  // center stays roughly where it was. Mobile-only (md:hidden); desktop
  // sidebar toggle lives in the sidebar rail.
  return (
    <Button
      data-sidebar="trigger"
      data-slot="sidebar-trigger"
      variant="ghost"
      size="icon-sm"
      className="-ml-1.5 mr-1 size-9 md:hidden"
      onClick={sidebar.toggleSidebar}
    >
      <Menu />
      <span className="sr-only">{t(($) => $.toggle_sidebar)}</span>
    </Button>
  );
}

interface PageHeaderProps {
  children: React.ReactNode;
  className?: string;
}

export function PageHeader({ children, className }: PageHeaderProps) {
  return (
    <div className={cn("flex h-12 shrink-0 items-center border-b px-4", className)}>
      <MobileSidebarTrigger />
      {children}
    </div>
  );
}
