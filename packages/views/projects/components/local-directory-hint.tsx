/**
 * Local-directory execution hints required a native host to identify the
 * current daemon. Existing resources remain visible in the project sidebar,
 * but the browser cannot make that machine-local assertion.
 */
export function LocalDirectoryHint({
  projectId: _projectId,
}: {
  projectId: string | null | undefined;
}) {
  return null;
}
