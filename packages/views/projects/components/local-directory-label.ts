import type {
  LocalDirectoryResourceRef,
  ProjectResource,
} from "@multica/core/types";

/**
 * Display name for a local_directory row.
 *
 * A row's name can live in two places: the top-level `label` column (what a
 * label update through the API or CLI writes; the panel itself no longer
 * offers a rename) and the legacy copy inside the ref (the only home desktop
 * builds up to v0.4.28 knew). Current servers keep the two converged on every
 * write, but rows last written by an older server can still disagree, and
 * rows created by older clients carry only the ref copy. The column outranks
 * the ref copy because it is the one current writes land in; the path is the
 * last resort so a row never renders nameless.
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
