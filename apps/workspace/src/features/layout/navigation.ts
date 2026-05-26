"use client";

import {
  Bot,
  Inbox,
  CalendarDays,
  CalendarRange,
  ListTodo,
  FolderKanban,
  Settings,
  CircleUser,
  Columns3,
  Clock,
  Users,
  Timer,
} from "lucide-react";

export const primaryNav = [
  { href: "/issues", label: "Issues", icon: ListTodo },
  { href: "/agents", label: "Agents", icon: Bot },
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/board", label: "Board", icon: Columns3 },
  { href: "/backlog", label: "Backlog", icon: ListTodo },
  { href: "/today", label: "Today", icon: CalendarDays },
  { href: "/upcoming", label: "Upcoming", icon: CalendarRange },
  { href: "/my-work", label: "My Work", icon: CircleUser },
  { href: "/my-time", label: "My Time", icon: Clock },
  { href: "/team-time", label: "Team Time", icon: Users },
  { href: "/pomodoro", label: "Pomodoro", icon: Timer },
  { href: "/calendar", label: "Calendar", icon: CalendarDays },
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
      return pathname === "/my-time" || pathname.startsWith("/my-time/");
    case "/team-time":
      return pathname === "/team-time";
    case "/calendar":
      return pathname === "/calendar";
    case "/agents":
      return pathname === "/agents" || pathname.startsWith("/agents/");
    case "/projects":
      return pathname === "/projects" || pathname.startsWith("/projects/");
    default:
      return pathname === href;
  }
}
