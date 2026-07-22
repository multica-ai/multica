export { paths, isGlobalPath } from "./paths";
export type { WorkspacePaths } from "./paths";
export { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";
export {
  WORKSPACE_PAGES,
  DEFAULT_ROUTE_ICON_NAME,
  resolveRouteIconName,
  pageForSegment,
} from "./route-icons";
export type {
  RouteIconName,
  NavLabelKey,
  WorkspacePageKey,
  WorkspacePage,
} from "./route-icons";
export {
  parseTabSubject,
  tabSubjectKey,
} from "./tab-subject";
export type { TabSubject, TabActorType } from "./tab-subject";
export {
  resolveTabPresentation,
  DEFAULT_TAB_VISUAL,
} from "./tab-presentation";
export type {
  TabVisual,
  TabTitleSpec,
  TabPresentation,
  TabEntityData,
  TabLabelKey,
} from "./tab-presentation";
export { resolvePostAuthDestination, useHasOnboarded } from "./resolve";
export {
  WorkspaceSlugProvider,
  useWorkspaceSlug,
  useRequiredWorkspaceSlug,
  useCurrentWorkspace,
  useWorkspacePaths,
} from "./hooks";
