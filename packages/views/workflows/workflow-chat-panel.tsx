"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Loader2, RotateCw } from "lucide-react";
import type { Agent, ChatMessage } from "@multica/core/types";
import { isAgentRuntimeBound, useAgentPresenceDetail } from "@multica/core/agents";
import { chatKeys, chatMessagesPageOptions, pendingChatTaskOptions } from "@multica/core/chat/queries";
import { hideQueuedChatMessages } from "@multica/core/chat/pending";
import { upsertChatMessageToCaches } from "@multica/core/chat/message-cache";
import { useWorkflowChatSession, useSendWorkflowChatMessage, type Workflow } from "@multica/core/workflows";
import { Button } from "@multica/ui/components/ui/button";
import { AgentPicker } from "../chat/components/new-chat-button";
import { ChatInput } from "../chat/components/chat-input";
import { ChatMessageList } from "../chat/components/chat-message-list";
import { seedAcceptedPendingTask } from "../chat/components/use-chat-controller";
import { useT } from "../i18n";

/** Hide completed and partially streamed protocol blocks; the server alone applies them. */
export function stripWorkflowProtocol(content: string): string {
  return content.replace(/<workflow_edit\b[^>]*>[\s\S]*?(?:<\/workflow_edit\s*>|$)/gi, "").trim();
}

export function WorkflowChatPanel({ wsId, workflow, selectedNodeId, agents, userId, save, readOnly = false }: {
  wsId: string;
  workflow: Workflow;
  selectedNodeId: string | null;
  agents: Agent[];
  userId?: string;
  save: () => Promise<Workflow>;
  readOnly?: boolean;
}) {
  const { t } = useT("workflows");
  const queryClient = useQueryClient();
  const [agentId, setAgentId] = useState<string | null>(null);
  const selectedAgent = agents.find((agent) => agent.id === agentId) ?? (agentId === null ? agents.find(isAgentRuntimeBound) : undefined);
  const [binding, setBinding] = useState<{ agentId: string; sessionId: string } | null>(null);
  const [sessionError, setSessionError] = useState(false);
  const [retrySession, setRetrySession] = useState(0);
  const [pendingContent, setPendingContent] = useState<string | null>(null);
  const [sendError, setSendError] = useState<string | null>(null);
  const pendingIdempotencyKey = useRef<{ content: string; key: string } | null>(null);
  const { mutateAsync: restoreSession } = useWorkflowChatSession(wsId, workflow.id);
  const send = useSendWorkflowChatMessage(wsId, workflow.id);
  const sessionId = binding?.agentId === selectedAgent?.id ? binding?.sessionId ?? "" : "";
  const presence = useAgentPresenceDetail(wsId, selectedAgent?.id);
  const availability = presence === "loading" ? undefined : presence.availability;

  useEffect(() => {
    let current = true;
    setSessionError(false);
    if (!selectedAgent?.id || !isAgentRuntimeBound(selectedAgent)) return;
    const id = selectedAgent.id;
    restoreSession({ agentId: id }).then((session) => {
      if (current) setBinding({ agentId: id, sessionId: session.sessionId });
    }).catch(() => { if (current) setSessionError(true); });
    return () => { current = false; };
  }, [selectedAgent?.id, restoreSession, retrySession]);

  const pages = useInfiniteQuery({ ...chatMessagesPageOptions(sessionId), refetchInterval: sessionId ? 3_000 : false });
  const pending = useQuery({ ...pendingChatTaskOptions(sessionId), refetchInterval: sessionId ? 3_000 : false });
  const messages = useMemo(() => hideQueuedChatMessages([...(pages.data?.pages ?? [])].reverse().flatMap((page) => page.messages), pending.data), [pages.data, pending.data]);
  // The pending-task endpoint returns `{ supports_queue: true }` even when
  // there is no active task. Only a concrete task id means the composer must
  // switch to the stop action.
  const isRunning = !!pending.data?.task_id;
  const canSend = !readOnly && !!sessionId && !!selectedAgent && isAgentRuntimeBound(selectedAgent) && !send.isPending;

  async function handleSend(content: string, _attachmentIds?: string[], commitInput?: (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void): Promise<boolean> {
    if (!canSend || isRunning) return false;
    setPendingContent(content);
    setSendError(null);
    try {
      const saved = await save();
      const existing = pendingIdempotencyKey.current;
      const idempotencyKey = existing?.content === content ? existing.key : crypto.randomUUID();
      pendingIdempotencyKey.current = { content, key: idempotencyKey };
      const result = await send.mutateAsync({ sessionId, content, selectedNodeId: selectedNodeId ?? undefined, expectedRevision: saved.revision, idempotencyKey });
      const message: ChatMessage = { id: result.message_id, chat_session_id: sessionId, role: "user", content, task_id: result.task_id, created_at: result.created_at };
      upsertChatMessageToCaches(queryClient, sessionId, message, { seedIfMissing: true });
      seedAcceptedPendingTask(queryClient, sessionId, { task_id: result.task_id, created_at: result.created_at, message_id: result.message_id, content, supports_queue: result.supports_queue, queued: result.queued });
      commitInput?.({ clearEditor: true });
      void queryClient.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      pendingIdempotencyKey.current = null;
      return true;
    } catch (error) {
      setSendError(error instanceof Error ? error.message : t(($) => $.send_failed));
      return false;
    } finally { setPendingContent(null); }
  }

  const picker = <AgentPicker agents={agents} userId={userId} currentAgentId={selectedAgent?.id} onSelect={(agent) => { setAgentId(agent.id); setSendError(null); }} trigger={<><Bot className="size-4" /><span className="max-w-48 truncate">{selectedAgent?.name ?? t(($) => $.select_agent)}</span></>} triggerRender={<Button variant="ghost" size="sm" disabled={send.isPending} />} />;
  return (
    <aside className="flex h-full min-h-0 w-full flex-col border-l bg-background md:w-96 md:shrink-0" aria-label={t(($) => $.chat)}>
      <div className="space-y-2 p-4"><h2 className="text-body font-semibold">{t(($) => $.chat)}</h2><p className="text-caption text-muted-foreground">{readOnly ? t(($) => $.unsupported_node_readonly, { types: "" }) : t(($) => $.chat_scope)}</p>{picker}
        {selectedNodeId && <p className="truncate text-micro text-muted-foreground">{t(($) => $.selected, { name: workflow.graph.nodes.find((node) => node.id === selectedNodeId)?.label ?? selectedNodeId })}</p>}
      </div>
      {sessionError && <div role="alert" className="mx-4 rounded-lg border border-destructive/40 p-3 text-caption"><p>{t(($) => $.session_error)}</p><Button size="sm" variant="ghost" onClick={() => setRetrySession((value) => value + 1)}><RotateCw className="size-3" />{t(($) => $.retry)}</Button></div>}
      {workflow.lastEditError && <p role="alert" className="mx-4 mb-2 rounded-lg border border-destructive/40 p-3 text-caption text-destructive">{t(($) => $.edit_failed, { error: workflow.lastEditError })}</p>}
      <div className="relative min-h-0 flex-1">
        {!messages.length && !isRunning ? <div className="px-6 py-10 text-caption leading-relaxed text-muted-foreground">{agents.length ? t(($) => $.chat_hint) : t(($) => $.no_agent)}</div> : <ChatMessageList messages={messages} pendingTask={pending.data} availability={availability} transformContent={stripWorkflowProtocol} hasOlderMessages={pages.hasNextPage} isFetchingOlderMessages={pages.isFetchingNextPage} onLoadOlderMessages={() => { void pages.fetchNextPage(); }} />}
      </div>
      {pendingContent && <div role="status" className="mx-4 mb-2 rounded-lg bg-muted p-3 text-caption"><p className="line-clamp-3 whitespace-pre-wrap">{pendingContent}</p><span className="mt-2 flex items-center gap-2 text-muted-foreground"><Loader2 className="size-3 animate-spin" />{t(($) => $.send_pending)}</span></div>}
      {sendError && <div role="alert" className="mx-4 mb-2 rounded-lg border border-destructive/40 p-3 text-caption text-destructive"><p>{t(($) => $.send_failed)}</p><p className="mt-1 break-words">{sendError}</p></div>}
      <ChatInput key={`${workflow.id}:${selectedAgent?.id ?? "none"}`} draftKeyOverride={`workflow:${wsId}:${workflow.id}:${userId ?? ""}:${selectedAgent?.id ?? "none"}`} onSend={handleSend} disabled={!canSend || !!pendingContent} noAgent={!agents.length} isRunning={isRunning} uploadEnabled={false} agentName={selectedAgent?.name} />
    </aside>
  );
}
