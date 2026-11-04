"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, History, Plus, Send } from "@multica/ui/icons";
import { api, ApiError } from "@multica/core/api";
import { chatQuickActionsPendingOptions } from "@multica/core/chat/queries";
import { useQuickActionsPendingTimeout } from "@multica/core/chat/use-quick-actions-pending-timeout";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ChatConversation } from "../../chat/components/chat-conversation";
import { useQuickActionsFailureToast } from "../../chat/components/use-quick-actions-failure-toast";
import { useChatController } from "../../chat/components/use-chat-controller";
import { useT } from "../../i18n";

type Mode = "talk" | "create";

/**
 * The agent's own chat, in two modes. "Talk" is a conversation with the agent,
 * new by default with the earlier ones one click away; it is the same
 * conversation the Chat tab shows. "Create" sends a request the agent writes up
 * as an issue and takes on itself, the way quick create does.
 */
export function CompanyAgentChat({
  agent,
  projectId,
  active,
}: {
  agent: Agent;
  projectId?: string;
  /** The pane is on screen: gates marking replies read. */
  active: boolean;
}) {
  const { t } = useT("issues");
  const [mode, setMode] = useState<Mode>("talk");
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b p-2" role="tablist" aria-label={agent.name}>
        <ModeButton selected={mode === "talk"} onClick={() => setMode("talk")}>
          {t(($) => $.company.mode_talk)}
        </ModeButton>
        <ModeButton selected={mode === "create"} onClick={() => setMode("create")}>
          {t(($) => $.company.mode_create)}
        </ModeButton>
      </div>
      {/* The conversation stays mounted so switching mode keeps the draft and the session. */}
      <div className={cn("min-h-0 flex-1 flex-col", mode === "talk" ? "flex" : "hidden")}>
        <TalkPane agent={agent} active={active && mode === "talk"} />
      </div>
      {mode === "create" ? <CreatePane agent={agent} projectId={projectId} /> : null}
    </div>
  );
}

function ModeButton({ selected, onClick, children }: { selected: boolean; onClick: () => void; children: string }) {
  return (
    <Button type="button" role="tab" size="sm" variant={selected ? "secondary" : "ghost"} aria-selected={selected} onClick={onClick}>
      {children}
    </Button>
  );
}

function TalkPane({ agent, active }: { agent: Agent; active: boolean }) {
  const { t } = useT("issues");
  const { t: tChat } = useT("chat");
  const c = useChatController({ isActive: active });
  const { data: quickActionsPending = null } = useQuery(chatQuickActionsPendingOptions(c.activeSessionId ?? ""));
  useQuickActionsPendingTimeout(c.activeSessionId ?? null, quickActionsPending);
  useQuickActionsFailureToast(c.activeSessionId ?? null);

  // A fresh conversation is started once per agent, as soon as the agent can be chatted with.
  const startedFor = useRef<string | null>(null);
  const chattable = c.availableAgents.find((candidate) => candidate.id === agent.id);
  const { handleStartNewChat } = c;
  useEffect(() => {
    if (!chattable || startedFor.current === agent.id) return;
    startedFor.current = agent.id;
    handleStartNewChat(chattable);
  }, [chattable, agent.id, handleStartNewChat]);

  if (!chattable) {
    return c.agentsSettled ? (
      <p className="p-4 text-caption text-muted-foreground">{t(($) => $.company.chat_unavailable)}</p>
    ) : null;
  }

  const earlier = c.sessions.filter((session) => session.agent_id === agent.id);
  return (
    <ChatConversation
      controller={c}
      quickActionsPending={quickActionsPending}
      onProjectChange={c.handleProjectChange}
      header={
        <div className="flex shrink-0 items-center gap-1 border-b px-2 py-1">
          <Button type="button" variant="ghost" size="xs" onClick={() => c.handleStartNewChat(chattable)}>
            <Plus className="size-3.5" />
            {t(($) => $.company.chat_new)}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button type="button" variant="ghost" size="xs" />}>
              <History className="size-3.5" />
              {t(($) => $.company.chat_history)}
              <ChevronDown className="size-3 text-muted-foreground" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-72 w-64 overflow-y-auto">
              {earlier.length === 0 ? (
                <DropdownMenuItem disabled>{t(($) => $.company.chat_history_empty)}</DropdownMenuItem>
              ) : (
                earlier.map((session) => (
                  <DropdownMenuItem
                    key={session.id}
                    onClick={() => c.handleSelectSession(session)}
                    className={cn(session.id === c.activeSessionId && "bg-accent")}
                  >
                    <span className="min-w-0 flex-1 truncate">
                      {session.title?.trim() || tChat(($) => $.window.untitled)}
                    </span>
                  </DropdownMenuItem>
                ))
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => c.handleStartNewChat(chattable)}>
                <Plus className="size-3.5" />
                {t(($) => $.company.chat_new)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      }
    />
  );
}

/** What the agent was asked to write up this time the pane was open. */
interface SentRequest {
  id: number;
  prompt: string;
}

function CreatePane({ agent, projectId }: { agent: Agent; projectId?: string }) {
  const { t } = useT("issues");
  const [prompt, setPrompt] = useState("");
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState<SentRequest[]>([]);
  const nextId = useRef(0);

  const send = async () => {
    const text = prompt.trim();
    if (!text || sending) return;
    setSending(true);
    try {
      await api.quickCreateIssue({ agent_id: agent.id, prompt: text, ...(projectId ? { project_id: projectId } : {}) });
      setSent((list) => [...list, { id: ++nextId.current, prompt: text }]);
      setPrompt("");
      toast.success(t(($) => $.company.create_sent, { name: agent.name }));
    } catch (error) {
      toast.error(error instanceof ApiError && error.message ? error.message : t(($) => $.company.create_failed));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ul className="min-h-0 flex-1 space-y-2 overflow-y-auto p-3">
        {sent.map((request) => (
          <li key={request.id} className="ml-auto max-w-[85%] rounded-lg bg-secondary px-3 py-2 text-body">
            {request.prompt}
          </li>
        ))}
      </ul>
      <div className="flex shrink-0 items-end gap-2 border-t p-2">
        <Textarea
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
              event.preventDefault();
              void send();
            }
          }}
          placeholder={t(($) => $.company.create_placeholder, { name: agent.name })}
          aria-label={t(($) => $.company.create_placeholder, { name: agent.name })}
          className="min-h-16 flex-1 resize-none"
        />
        <Button type="button" size="sm" disabled={!prompt.trim() || sending} onClick={() => void send()}>
          <Send className="size-4" />
          {t(($) => $.company.create_send)}
        </Button>
      </div>
    </div>
  );
}
