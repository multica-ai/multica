import { WorkspaceEntry } from "@multica/views/links";
import { useTabStore } from "@/stores/tab-store";

// Resolve the entry in the session layer, never via router Navigate/redirect.
// The coordinator remains the only writer to the memory router (MUL-4741).
function replaceEntry(path: string) {
  useTabStore.getState().navigateActiveSession(path, { replace: true });
}

export function DesktopWorkspaceEntry() {
  return <WorkspaceEntry onResolve={replaceEntry} />;
}
