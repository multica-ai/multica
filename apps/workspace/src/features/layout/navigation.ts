"use client";

import {
  Inbox,
  ListTodo,
  Bot,
  Monitor,
  Settings,
  BookOpenText,
  CircleUser,
} from "lucide-react";

export const primaryNav = [
  { href: "/inbox", label: "Inbox", icon: Inbox },
  { href: "/my-issues", label: "My Issues", icon: CircleUser },
  { href: "/issues", label: "Issues", icon: ListTodo },
];

export const workspaceNav = [
  { href: "/agents", label: "Agents", icon: Bot },
  { href: "/runtimes", label: "Runtimes", icon: Monitor },
  { href: "/skills", label: "Skills", icon: BookOpenText },
  { href: "/settings", label: "Settings", icon: Settings },
];

export function isWorkspaceNavActive(pathname: string, href: string): boolean {
  switch (href) {
    case "/inbox":
      return pathname === "/" || pathname === "/inbox";
    case "/issues":
      return pathname === "/issues" || pathname === "/board" || pathname.startsWith("/issues/");
    case "/agents":
      return pathname === "/agents" || pathname.startsWith("/agents/");
    default:
      return pathname === href;
  }
}
