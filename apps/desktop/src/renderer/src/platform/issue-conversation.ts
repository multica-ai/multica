import { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import { localV2SummarySchema, type LocalV2Summary } from "@multica/core/local-conversation/v2";
import type { Issue } from "@multica/core/types";
import { issueConversationDraftKey, issueConversationScopeKey, useIssueConversationStore } from "../stores/issue-conversation-store";

const providersSchema = z.object({
  providers: z.array(z.object({ id: z.string(), available: z.boolean().default(false) })).default([]),
});

export interface IssueConversationOptions {
  issue: Pick<Issue, "id" | "identifier" | "workspace_id">;
  userId: string;
  deviceId: string;
  service: {
    request(method: string, path: string, body?: unknown): Promise<unknown>;
    receiveSummary(summary: LocalV2Summary): void;
  };
  isCurrent(): boolean;
  open(summary: LocalV2Summary): void;
  unavailableMessage: string;
}

export function issueConversationPendingKey(userId: string, issue: IssueConversationOptions["issue"]) {
  return JSON.stringify([issueConversationScopeKey({ userId, workspaceId: issue.workspace_id }), issue.id]);
}

export async function launchIssueConversation(options: IssueConversationOptions): Promise<void> {
  const { issue, userId, deviceId, service, isCurrent, open } = options;
  const store = useIssueConversationStore.getState();
  const pendingKey = issueConversationPendingKey(userId, issue);
  if (!isCurrent() || !store.begin(pendingKey)) return;
  try {
    const { providers } = parseWithFallback<z.infer<typeof providersSchema>>(await service.request("GET", "/providers"), providersSchema, { providers: [] }, { endpoint: "local-v2.providers" });
    if (!isCurrent()) return;
    const provider = providers.find((entry) => entry.available && entry.id === "codex")
      ?? providers.find((entry) => entry.available && entry.id === "claude");
    if (!provider) throw new Error(options.unavailableMessage);
    const summary = localV2SummarySchema.parse(await service.request("POST", "/sessions", {
      id: crypto.randomUUID(), provider: provider.id, title: issue.identifier,
    }));
    service.receiveSummary(summary);
    if (!isCurrent()) return;
    const sessionId = summary.conversation_id;
    const scope = { userId, workspaceId: issue.workspace_id };
    store.offer(issueConversationDraftKey(scope, deviceId, sessionId), {
      id: crypto.randomUUID(),
      sessionId,
      content: `[${issue.identifier}](mention://workspace-issue/${issue.id}?${new URLSearchParams({ workspaceId: issue.workspace_id })}) `,
    });
    open(summary);
  } finally {
    store.finish(pendingKey);
  }
}
