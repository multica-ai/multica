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
  Server,
  BookOpenText,
  Settings,
  File,
  FileText,
  FileImage,
  FileCode,
  FileArchive,
  FileAudio,
  FileVideo,
  FileQuestion,
  type LucideIcon,
} from "lucide-react";
import { resolveRouteIconName, type RouteIconName } from "@multica/core/paths";

/**
 * Icon name → component registry: the rendering half of the route icon
 * contract defined in `@multica/core/paths`.
 *
 * Every {@link RouteIconName} must have an entry — the `Record` type makes a
 * missing key a compile error.
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
  Server,
  BookOpenText,
  Settings,
  File,
  FileText,
  FileImage,
  FileCode,
  FileArchive,
  FileAudio,
  FileVideo,
  FileQuestion,
};

/**
 * Resolve the icon component for a workspace-scoped path or full tab URL.
 *
 * This is the only entry point navigation surfaces should use: it takes the
 * path they already have rather than an icon name, so no caller has to hold,
 * persist, or cast an icon name. `resolveRouteIconName` always returns a
 * valid name and the registry is total, so the result is never undefined — an
 * unknown route falls back to the default icon instead of rendering nothing.
 *
 * The sidebar nav and the desktop tab bar both call this, which is what keeps
 * a route's icon identical in both places.
 */
export function routeIconForPath(path: string): LucideIcon {
  return ROUTE_ICON_COMPONENTS[resolveRouteIconName(path)];
}
