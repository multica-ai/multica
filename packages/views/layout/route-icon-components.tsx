import {
  Inbox,
  MessageSquare,
  CircleUser,
  ListTodo,
  FolderKanban,
  Zap,
  Bot,
  Users,
  BarChart3,
  Monitor,
  BookOpenText,
  Settings,
  type LucideIcon,
} from "lucide-react";
import type { RouteIconName } from "@multica/core/paths";

/**
 * Icon name → component registry for route icons.
 *
 * This is the *rendering* half of the single source of truth defined in
 * `@multica/core/paths` (`ROUTE_ICON_NAMES` / `resolveRouteIconName`). Consumed
 * by both the sidebar nav (app-sidebar.tsx) and the desktop tab bar so a
 * route's icon is identical in both places.
 *
 * Every {@link RouteIconName} must have an entry here — the `Record` type makes
 * a missing key a compile error, and `route-icons.consistency` re-checks it.
 */
export const ROUTE_ICON_COMPONENTS: Record<RouteIconName, LucideIcon> = {
  Inbox,
  MessageSquare,
  CircleUser,
  ListTodo,
  FolderKanban,
  Zap,
  Bot,
  Users,
  BarChart3,
  Monitor,
  BookOpenText,
  Settings,
};
