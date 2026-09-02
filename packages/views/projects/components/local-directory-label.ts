import type {
  LocalDirectoryResourceRef,
  ProjectResource,
} from "@multica/core/types";

/**
 * Display name for a local_directory row.
 *
 * A row's name can live in two places: the top-level `label` column (written
 * by renames since they stopped resending the ref — see
 * handleRenameLocalDirectory in project-resources-section.tsx) and the legacy
 * copy inside the ref (the only home desktop builds up to v0.4.28 knew).
 * Current servers keep the two converged on every write, but rows last
 * written by an older server can still disagree, and rows created by older
 * clients carry only the ref copy. The column outranks the ref copy because
 * it is the one current-client renames write; the path is the last resort so
 * a row never renders nameless.
 */
export function localDirectoryLabel(
  resource: Pick<ProjectResource, "label"> & {
    resource_ref: LocalDirectoryResourceRef;
  },
): string {
  const ref = resource.resource_ref;
  return (
    (resource.label || ref.label || ref.local_path).trim() ||
    ref.local_path.trim()
  );
}
