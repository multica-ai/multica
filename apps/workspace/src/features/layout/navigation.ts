"use client";

import {
  Inbox,
  CalendarDays,
  CalendarRange,
  ListTodo,
  FolderKanban,
  Settings,
  CircleUser,
  Columns3,
  Clock,
} from "lucide-react";

export const primaryNav = [
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/issues", label: "Issues", icon: ListTodo },
  { href: "/board", label: "Board", icon: Columns3 },
  { href: "/backlog", label: "Backlog", icon: ListTodo },
  { href: "/today", label: "Today", icon: CalendarDays },
  { href: "/upcoming", label: "Upcoming", icon: CalendarRange },
  { href: "/my-work", label: "My Work", icon: CircleUser },
  { href: "/my-time", label: "My Time", icon: Clock },
  { href: "/notifications", label: "Notifications", icon: Inbox },
];

export const workspaceNav = [
  { href: "/settings", label: "Settings", icon: Settings },
];

export function isWorkspaceNavActive(pathname: string, href: string): boolean {
  switch (href) {
    case "/issues":
      return pathname === "/issues" || pathname.startsWith("/issues/");
    case "/board":
      return pathname === "/board";
    case "/notifications":
      return pathname === "/" || pathname === "/inbox" || pathname === "/notifications";
    case "/my-work":
      return pathname === "/my-work" || pathname === "/my-issues";
    case "/my-time":
      return pathname === "/my-time";
    case "/agents":
      return pathname === "/agents" || pathname.startsWith("/agents/");
    case "/projects":
      return pathname === "/projects" || pathname.startsWith("/projects/");
    default:
      return pathname === href;
  }
}
