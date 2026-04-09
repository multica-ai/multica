"use client";

import { createContext, useContext } from "react";

const WorkspaceIdContext = createContext<string | null>(null);

export function WorkspaceIdProvider({
  wsId,
  children,
}: {
  wsId: string;
  children: React.ReactNode;
}) {
  return (
    <WorkspaceIdContext.Provider value={wsId}>
      {children}
    </WorkspaceIdContext.Provider>
  );
}

export function useWorkspaceId(): string {
  const wsId = useContext(WorkspaceIdContext);
  if (!wsId) throw new Error("useWorkspaceId: no workspace selected — wrap in WorkspaceIdProvider");
  return wsId;
}
