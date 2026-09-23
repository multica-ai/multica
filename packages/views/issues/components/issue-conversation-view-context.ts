"use client";

import { createContext, useContext } from "react";

/** Issue-tab presentation callback; no routing or execution state. */
export const IssueConversationViewContext = createContext<{
  issueId: string;
  open: (runId: string) => void;
} | null>(null);

export function useIssueConversationView() {
  return useContext(IssueConversationViewContext);
}
