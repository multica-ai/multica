// CEREBRO-PATCH(core-projects-index): cerebro modification of upstream file
export { projectKeys, projectListOptions, projectDetailOptions } from "./queries";
// CEREBRO-PATCH(nested-projects): export fork-only nested project hooks.
export {
  projectNestingKeys,
  projectTreeOptions,
  projectRollupStatsOptions,
  buildProjectTree,
  useSetProjectParent,
  useSetProjectShowDescendants,
} from "./nesting";
export { useCreateProject, useUpdateProject, useDeleteProject } from "./mutations";
export { PROJECT_COLORS, getProjectColor } from "./config";
export { useProjectDraftStore } from "./draft-store";
export {
  projectResourceKeys,
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
} from "./resource-queries";
