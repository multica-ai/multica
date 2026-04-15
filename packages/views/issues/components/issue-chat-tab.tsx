"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { ChatMessageList } from "../../chat/components/chat-message-list";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWS } from "@multica/core/realtime";
import { agentListOptions } from "@multica/core/workspace/queries";
import { issueChatSessionsOptions, chatKeys } from "@multica/core/chat/queries";
import type {
  ChatMessage,
  SendChatMessageResponse,
  TaskMessagePayload,
  ChatDonePayload,
} from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { MessageSquare, Pencil, ChevronDown } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";


interface IssueChatTabProps {
  issueId: string;
}

export function IssueChatTab({ issueId }: IssueChatTabProps) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null);
  const [pendingTaskId, setPendingTaskId] = useState<string | null>(null);
  const [timelineItems, setTimelineItems] = useState<ChatTimelineItem[]>([]);
  const [editingTitle, setEditingTitle] = useState(false);

  const { data: sessions = [] } = useQuery(issueChatSessionsOptions(issueId));

  // Auto-select most recent session
  useEffect(() => {
    if (sessions.length > 0 && !activeSessionId) {
      setActiveSessionId(sessions[0]!.id);
    }
  }, [sessions, activeSessionId]);

  // Fetch messages for active session
  const { data: messages = [] } = useQuery({
    queryKey: chatKeys.messages(activeSessionId ?? ""),
    queryFn: () => api.listChatMessages(activeSessionId!),
    enabled: !!activeSessionId,
    staleTime: Infinity,
  });

  // Create session mutation
  const createSession = useMutation({
    mutationFn: async (agentId: string) => {
      return api.createIssueChatSession(issueId, {
        agent_id: agentId,
        title: "Issue chat",
      });
    },
    onSuccess: (session) => {
      qc.invalidateQueries({ queryKey: ["issue-chat-sessions", issueId] });
      setActiveSessionId(session.id);
      setTimelineItems([]);
      setPendingTaskId(null);
    },
  });

  // Use ref for pendingTaskId so WS handlers always see the latest value
  const pendingTaskRef = useRef<string | null>(pendingTaskId);
  pendingTaskRef.current = pendingTaskId;

  const activeSessionRef = useRef<string | null>(activeSessionId);
  activeSessionRef.current = activeSessionId;

  const { subscribe } = useWS();

  useEffect(() => {
    const matchesPending = (taskId: string) =>
      !!pendingTaskRef.current && taskId === pendingTaskRef.current;

    const finalizePending = (invalidateCache: boolean) => {
      if (invalidateCache) {
        const sid = activeSessionRef.current;
        if (sid) {
          qc.invalidateQueries({ queryKey: chatKeys.messages(sid) });
        }
      }
      setTimelineItems([]);
      setPendingTaskId(null);
    };

    const unsubMessage = subscribe("task:message", (payload) => {
      const p = payload as TaskMessagePayload;
      if (!matchesPending(p.task_id)) return;
      setTimelineItems((prev) => [
        ...prev,
        {
          seq: p.seq,
          type: p.type,
          tool: p.tool,
          content: p.content,
          input: p.input,
          output: p.output,
        },
      ]);
    });

    const unsubDone = subscribe("chat:done", (payload) => {
      const p = payload as ChatDonePayload;
      if (!matchesPending(p.task_id)) return;
      finalizePending(true);
    });

    const unsubCompleted = subscribe("task:completed", (payload) => {
      const p = payload as { task_id: string };
      if (!matchesPending(p.task_id)) return;
      finalizePending(true);
    });

    const unsubFailed = subscribe("task:failed", (payload) => {
      const p = payload as { task_id: string };
      if (!matchesPending(p.task_id)) return;
      finalizePending(false);
    });

    return () => {
      unsubMessage();
      unsubDone();
      unsubCompleted();
      unsubFailed();
    };
  }, [subscribe, qc]);

  // Send message
  const handleSend = useCallback(
    async (content: string) => {
      if (!activeSessionId) return;

      const finalContent = content;

      // Optimistic update
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: activeSessionId,
        role: "user",
        content: finalContent,
        task_id: null,
        created_at: new Date().toISOString(),
      };
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(activeSessionId),
        (old) => (old ? [...old, optimistic] : [optimistic]),
      );

      const result: SendChatMessageResponse = await api.sendChatMessage(
        activeSessionId,
        finalContent,
      );
      setPendingTaskId(result.task_id);
      qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
    },
    [activeSessionId, qc],
  );

  // Cancel task
  const handleStop = useCallback(async () => {
    if (!pendingTaskId) return;
    try {
      await api.cancelTaskById(pendingTaskId);
    } catch {
      // Task may already be completed
    }
    if (activeSessionId) {
      qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
    }
    setTimelineItems([]);
    setPendingTaskId(null);
  }, [pendingTaskId, activeSessionId, qc]);

  const handleRenameSubmit = useCallback(
    async (newTitle: string) => {
      if (!activeSessionId || !newTitle.trim()) return;
      await api.updateChatSessionTitle(activeSessionId, newTitle.trim());
      qc.invalidateQueries({ queryKey: ["issue-chat-sessions", issueId] });
      setEditingTitle(false);
    },
    [activeSessionId, issueId, qc],
  );

  const availableAgents = agents.filter((a) => !a.archived_at);
  const hasMessages = messages.length > 0 || timelineItems.length > 0;

  // Get current session's agent for command list
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const activeAgent = agents.find((a) => a.id === activeSession?.agent_id);

  const handleAgentChange = async (agentId: string) => {
    // Cancel running task before switching
    if (pendingTaskId) {
      try { await api.cancelTaskById(pendingTaskId); } catch {}
      setPendingTaskId(null);
      setTimelineItems([]);
    }

    const existing = sessions.find((s) => s.agent_id === agentId);
    if (existing) {
      setActiveSessionId(existing.id);
      setTimelineItems([]);
      setPendingTaskId(null);
    } else {
      createSession.mutate(agentId);
    }
  };

  // No sessions yet — show empty state with input area
  if (sessions.length === 0 && !activeSessionId) {
    return (
      <div className="flex flex-col">
        <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
          <MessageSquare className="h-4 w-4 mr-2 text-muted-foreground/50" />
          Start a chat with an agent on this issue
        </div>
        <div className="mt-2">
          {availableAgents.length === 0 ? (
            <div className="text-xs text-muted-foreground text-center py-2">
              No agents available
            </div>
          ) : (
            <IssueChatInput
              onSend={(content) => {
                if (availableAgents.length === 0) return;
                // Create session with the first agent by default, then send
                const agentId = availableAgents[0]!.id;
                createSession.mutate(agentId, {
                  onSuccess: (session) => {
                    // Send message after session is created
                    api.sendChatMessage(session.id, content).then((result) => {
                      setPendingTaskId(result.task_id);
                    });
                  },
                });
              }}
              onStop={handleStop}
              isRunning={false}
              disabled={false}
              agents={availableAgents}
              activeAgentId={availableAgents[0]!.id}
              onAgentChange={handleAgentChange}
            />
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      {/* Session header */}
      <div className="flex items-center gap-2 py-2">
        <select
          className="text-xs bg-transparent border rounded px-2 py-1 outline-none"
          value={activeSessionId || ""}
          onChange={(e) => {
            setActiveSessionId(e.target.value);
            setTimelineItems([]);
            setPendingTaskId(null);
            setEditingTitle(false);
          }}
        >
          {sessions.map((s) => {
            const agentName = agents.find((a) => a.id === s.agent_id)?.name ?? "Agent";
            const date = new Date(s.created_at).toLocaleDateString();
            return (
              <option key={s.id} value={s.id}>
                {agentName} — {s.title || "Chat"} — {date}
              </option>
            );
          })}
        </select>
        {activeSessionId && (
          <button
            onClick={() => setEditingTitle(true)}
            className="text-xs text-muted-foreground hover:text-foreground"
            title="Rename session"
          >
            <Pencil className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Inline rename input */}
      {editingTitle && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const input = e.currentTarget.elements.namedItem("title") as HTMLInputElement;
            handleRenameSubmit(input.value);
          }}
          className="py-1"
        >
          <input
            name="title"
            autoFocus
            defaultValue={activeSession?.title || ""}
            className="w-full text-xs border rounded px-2 py-1 outline-none"
            onBlur={(e) => handleRenameSubmit(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setEditingTitle(false);
            }}
          />
        </form>
      )}

      {/* Messages */}
      {hasMessages && (
        <ChatMessageList
          messages={messages}
          timelineItems={timelineItems}
          isWaiting={!!pendingTaskId}
        />
      )}

      {!hasMessages && (
        <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
          Send a message to start chatting
        </div>
      )}

      {/* Input */}
      <div className="mt-2">
        <IssueChatInput
          onSend={handleSend}
          onStop={handleStop}
          isRunning={!!pendingTaskId}
          disabled={!activeSessionId}
          agents={availableAgents}
          activeAgentId={activeAgent?.id ?? null}
          onAgentChange={handleAgentChange}
        />
      </div>
    </div>
  );
}

/**
 * Standalone chat input for issue chat — does NOT depend on the global
 * useChatStore draft, keeping it isolated from the floating chat widget.
 */
function IssueChatInput({
  onSend,
  onStop,
  isRunning,
  disabled,
  agents,
  activeAgentId,
  onAgentChange,
}: {
  onSend: (content: string) => void;
  onStop?: () => void;
  isRunning?: boolean;
  disabled?: boolean;
  agents: { id: string; name: string }[];
  activeAgentId: string | null;
  onAgentChange: (agentId: string) => void;
}) {
  const editorRef = useRef<ContentEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);
  const [showAgentPicker, setShowAgentPicker] = useState(false);
  const agentPickerRef = useRef<HTMLDivElement>(null);

  const activeAgentName = agents.find((a) => a.id === activeAgentId)?.name;

  const handleSend = () => {
    const content = editorRef.current
      ?.getMarkdown()
      ?.replace(/(\n\s*)+$/, "")
      .trim();
    if (!content || isRunning || disabled) return;
    onSend(content);
    editorRef.current?.clearContent();
    setIsEmpty(true);
  };

  // Close agent picker on click outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (agentPickerRef.current && !agentPickerRef.current.contains(e.target as Node)) {
        setShowAgentPicker(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  return (
    <div className="relative">
      <div
        className="relative flex min-h-16 max-h-40 flex-col rounded-lg bg-card pb-8 border border-border transition-colors focus-within:border-brand"
        onKeyDown={(e) => {
          // Enter sends, Shift+Enter for newline (chat-style)
          if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
            e.preventDefault();
            handleSend();
          }
        }}
      >
        <div className="flex-1 min-h-0 overflow-y-auto px-3 py-2">
          <ContentEditor
            ref={editorRef}
            placeholder={disabled ? "Select a session first" : "Ask the agent..."}
            onUpdate={(md) => setIsEmpty(!md.trim())}
            onSubmit={handleSend}
            debounceMs={100}
          />
        </div>
        <div className="absolute bottom-1 left-1.5 flex items-center gap-1">
          <div className="relative" ref={agentPickerRef}>
            <button
              onClick={() => !isRunning && setShowAgentPicker(!showAgentPicker)}
              disabled={isRunning}
              className={cn(
                "flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] text-muted-foreground hover:text-foreground hover:border-foreground/30 transition-colors",
                isRunning && "opacity-50 cursor-not-allowed",
              )}
            >
              {activeAgentName || "Agent"}
              <ChevronDown className="h-3 w-3" />
            </button>
            {showAgentPicker && (
              <div className="absolute bottom-full left-0 mb-1 rounded-lg border bg-popover shadow-md z-10 min-w-[140px] overflow-hidden">
                {agents.map((agent) => (
                  <button
                    key={agent.id}
                    onClick={() => {
                      onAgentChange(agent.id);
                      setShowAgentPicker(false);
                    }}
                    className={cn(
                      "flex w-full items-center gap-2 px-3 py-1.5 text-xs hover:bg-accent transition-colors text-left",
                      agent.id === activeAgentId && "bg-accent/50 font-medium",
                    )}
                  >
                    {agent.name}
                    {agent.id === activeAgentId && (
                      <span className="ml-auto text-[10px] text-muted-foreground">✓</span>
                    )}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
        <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
          <SubmitButton
            onClick={handleSend}
            disabled={isEmpty || !!disabled}
            running={isRunning}
            onStop={onStop}
          />
        </div>
      </div>
    </div>
  );
}
