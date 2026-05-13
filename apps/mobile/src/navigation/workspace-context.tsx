import { createContext, useContext } from "react";
import type { Workspace } from "@multica/core/types";

export const WorkspaceContext = createContext<{
  workspace: Workspace;
  setWorkspace: (workspace: Workspace) => void;
} | null>(null);

export function useMobileWorkspace() {
  const context = useContext(WorkspaceContext);
  if (!context) throw new Error("useMobileWorkspace must be used inside WorkspaceContext");
  return context;
}
