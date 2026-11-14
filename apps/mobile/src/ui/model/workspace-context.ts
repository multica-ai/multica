import { createContext, useContext } from "react";
import type { Workspace } from "@multica/core/types";

interface WorkspaceContextValue {
  workspace: Workspace;
  workspaces: readonly Workspace[];
}

export const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

/** The workspace the signed-in shell is showing; only mounted once one is chosen. */
export function useWorkspace(): WorkspaceContextValue {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error("useWorkspace must be used inside the workspace shell");
  return value;
}
