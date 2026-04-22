"use client";

import {
  Inbox,
  CalendarDays,
  CalendarRange,
  ListTodo,
  FolderKanban,
  Bot,
  Monitor,
  Settings,
  BookOpenText,
  CircleUser,
  Columns3,
} from "lucide-react";

export const primaryNav = [
  { href: "/issues", label: "Issues", icon: ListTodo },
  { href: "/board", label: "Board", icon: Columns3 },
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/backlog", label: "Backlog", icon: ListTodo },
  { href: "/today", label: "Today", icon: CalendarDays },
  { href: "/upcoming", label: "Upcoming", icon: CalendarRange },
  { href: "/my-work", label: "My Work", icon: CircleUser },
  { href: "/notifications", label: "Notifications", icon: Inbox },
];

export const workspaceNav = [
  { href: "/agents", label: "Agents", icon: Bot },
  { href: "/runtimes", label: "Runtimes", icon: Monitor },
  { href: "/skills", label: "Skills", icon: BookOpenText },
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
    case "/agents":
      return pathname === "/agents" || pathname.startsWith("/agents/");
    case "/projects":
      return pathname === "/projects" || pathname.startsWith("/projects/");
    default:
      return pathname === href;
  }
}
