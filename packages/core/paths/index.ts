export { paths, isGlobalPath } from "./paths";
export type { WorkspacePaths } from "./paths";
export { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";
export {
  ROUTE_ICON_NAMES,
  DEFAULT_ROUTE_ICON_NAME,
  resolveRouteIconName,
} from "./route-icons";
export type { RouteIconName } from "./route-icons";
export { resolvePostAuthDestination, useHasOnboarded } from "./resolve";
export {
  WorkspaceSlugProvider,
  useWorkspaceSlug,
  useRequiredWorkspaceSlug,
  useCurrentWorkspace,
  useWorkspacePaths,
} from "./hooks";
