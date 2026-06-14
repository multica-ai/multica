"use client";

import {
  Bot,
  Archive,
  type LucideIcon,
  Inbox,
	  CalendarDays,
	  CalendarRange,
	  ClipboardList,
  ListTodo,
  FolderKanban,
  Settings,
  CircleUser,
  Columns3,
  Timer,
  LogOut,
} from "lucide-react";

export interface WorkspaceNavItem {
  href: string;
  label: string;
  icon: LucideIcon;
}

export interface WorkspaceNavGroup {
  label: string;
  items: WorkspaceNavItem[];
}

export const navigationGroups: WorkspaceNavGroup[] = [
  {
    label: "Focus",
    items: [
      { href: "/notifications", label: "Inbox", icon: Inbox },
      { href: "/my-work", label: "My Work", icon: CircleUser },
      { href: "/issues", label: "Issues", icon: ListTodo },
      { href: "/issues/archived", label: "Archived", icon: Archive },
    ],
  },
  {
    label: "Planning",
    items: [
      { href: "/projects", label: "Projects", icon: FolderKanban },
	      { href: "/board", label: "Board", icon: Columns3 },
	      { href: "/plan", label: "Plan", icon: ClipboardList },
	      { href: "/backlog", label: "Backlog", icon: ListTodo },
      { href: "/today", label: "Today", icon: CalendarDays },
      { href: "/upcoming", label: "Upcoming", icon: CalendarRange },
      { href: "/calendar", label: "Calendar", icon: CalendarDays },
    ],
  },
  {
    label: "Tools",
    items: [{ href: "/focus", label: "Focus", icon: Timer }],
  },
  {
    label: "Workspace",
    items: [{ href: "/agents", label: "Agents", icon: Bot }],
  },
];

export const workspaceFooterNav: WorkspaceNavItem[] = [
  { href: "/settings", label: "Settings", icon: Settings },
  { href: "/logout", label: "Log out", icon: LogOut },
];

export function isWorkspaceNavActive(pathname: string, href: string): boolean {
  switch (href) {
    case "/issues":
      return pathname === "/issues" || (pathname.startsWith("/issues/") && pathname !== "/issues/archived");
    case "/issues/archived":
      return pathname === "/issues/archived";
    case "/board":
      return pathname === "/board";
    case "/notifications":
      return pathname === "/" || pathname === "/inbox" || pathname === "/notifications";
    case "/my-work":
      return pathname === "/my-work" || pathname === "/my-issues";
    case "/calendar":
      return pathname === "/calendar";
    case "/focus":
      return pathname === "/focus" || pathname === "/pomodoro";
    case "/agents":
      return pathname === "/agents" || pathname.startsWith("/agents/");
    case "/projects":
      return pathname === "/projects" || pathname.startsWith("/projects/");
    default:
      return pathname === href;
  }
}

export function getWorkspacePageTitle(pathname: string): string {
  for (const group of navigationGroups) {
    for (const item of group.items) {
      if (isWorkspaceNavActive(pathname, item.href)) {
        return item.label;
      }
    }
  }

  if (pathname === "/settings") {
    return "Settings";
  }

  return "Workspace";
}
