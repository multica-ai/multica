"use client";

import { createContext, useContext, type ComponentType, type ReactNode } from "react";
import type { Issue } from "@multica/core/types";

const IssueConversationActionContext = createContext<ComponentType<{ issue: Issue }> | null>(null);

export function IssueConversationActionProvider({ action, children }: {
  action: ComponentType<{ issue: Issue }>;
  children: ReactNode;
}) {
  return <IssueConversationActionContext.Provider value={action}>{children}</IssueConversationActionContext.Provider>;
}

export function IssueConversationAction({ issue }: { issue: Issue }) {
  const Action = useContext(IssueConversationActionContext);
  return Action ? <Action issue={issue} /> : null;
}
