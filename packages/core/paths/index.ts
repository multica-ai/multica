export { paths, isGlobalPath } from "./paths";
export type { WorkspacePaths } from "./paths";
export { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";
export {
  WorkspaceSlugProvider,
  useWorkspaceSlug,
  useRequiredWorkspaceSlug,
  useCurrentWorkspace,
  useWorkspacePaths,
} from "./hooks";
