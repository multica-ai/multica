"use client";

import { useState, useCallback, useRef, useEffect } from "react";
import { Send, Loader2, ExternalLink, Sparkles } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import { AppLink } from "../../navigation";
import { toast } from "sonner";
import type { CEOCommandResponse } from "@multica/core/types";

interface CommandEntry {
  id: string;
  message: string;
  status: "sending" | "success" | "error";
  result?: CEOCommandResponse;
  error?: string;
  timestamp: Date;
}

export function CommandPage() {
  const wsId = useWorkspaceId();
  const [input, setInput] = useState("");
  const [entries, setEntries] = useState<CommandEntry[]>([]);
  const [isSending, setIsSending] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  // Scroll to bottom when new entries arrive
  useEffect(() => {
    if (listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight;
    }
  }, [entries]);

  // Auto-focus input on mount
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const handleSend = useCallback(async () => {
    const message = input.trim();
    if (!message || isSending) return;

    const id = `cmd-${Date.now()}`;
    const entry: CommandEntry = {
      id,
      message,
      status: "sending",
      timestamp: new Date(),
    };

    setEntries((prev) => [...prev, entry]);
    setInput("");
    setIsSending(true);

    try {
      const result = await api.sendCEOCommand(message);
      setEntries((prev) =>
        prev.map((e) =>
          e.id === id ? { ...e, status: "success", result } : e,
        ),
      );
      toast.success("Command dispatched to CEO agent");
    } catch (err: any) {
      const errorMsg = err?.message || "Failed to send command";
      setEntries((prev) =>
        prev.map((e) =>
          e.id === id ? { ...e, status: "error", error: errorMsg } : e,
        ),
      );
      toast.error(errorMsg);
    } finally {
      setIsSending(false);
      inputRef.current?.focus();
    }
  }, [input, isSending]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  // Resolve agent name by ID
  const getAgentName = (id: string) => {
    const agent = agents.find((a) => a.id === id);
    return agent?.name ?? id.slice(0, 8);
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <header className="flex items-center gap-3 border-b px-6 py-4">
        <div className="flex items-center gap-2">
          <Sparkles className="size-5 text-brand" />
          <h1 className="text-lg font-semibold">CEO Command</h1>
        </div>
        <span className="text-sm text-muted-foreground">
          Send instructions and let the CEO agent plan &amp; delegate
        </span>
      </header>

      {/* Messages */}
      <div ref={listRef} className="flex-1 overflow-y-auto px-6 py-4 space-y-4">
        {entries.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-center text-muted-foreground gap-3">
            <Sparkles className="size-10 opacity-30" />
            <p className="text-lg font-medium">Tell CEO what you need</p>
            <p className="text-sm max-w-md">
              Type a command below. The CEO agent will create a plan and assign
              tasks to the appropriate agents automatically.
            </p>
          </div>
        )}

        {entries.map((entry) => (
          <div key={entry.id} className="space-y-2">
            {/* User message */}
            <div className="flex justify-end">
              <div className="max-w-[80%] rounded-xl bg-brand/10 px-4 py-2.5 text-sm">
                <p className="whitespace-pre-wrap">{entry.message}</p>
                <span className="mt-1 block text-[11px] text-muted-foreground">
                  {entry.timestamp.toLocaleTimeString()}
                </span>
              </div>
            </div>

            {/* Result / status */}
            <div className="flex justify-start">
              <div className="max-w-[80%] rounded-xl ring-1 ring-border px-4 py-2.5 text-sm">
                {entry.status === "sending" && (
                  <div className="flex items-center gap-2 text-muted-foreground">
                    <Loader2 className="size-4 animate-spin" />
                    <span>Dispatching to CEO agent…</span>
                  </div>
                )}
                {entry.status === "error" && (
                  <p className="text-destructive">{entry.error}</p>
                )}
                {entry.status === "success" && entry.result && (
                  <div className="space-y-2">
                    <p className="text-green-600 dark:text-green-400 font-medium">
                      ✓ Command dispatched — CEO is planning
                    </p>
                    <div className="grid gap-1 text-xs text-muted-foreground">
                      {entry.result.issue_id && (
                        <div className="flex items-center gap-1">
                          <span>Issue:</span>
                          <AppLink
                            href={`/issues/${entry.result.issue_id}`}
                            className="inline-flex items-center gap-0.5 text-brand hover:underline"
                          >
                            {entry.result.issue_id.slice(0, 8)}…
                            <ExternalLink className="size-3" />
                          </AppLink>
                        </div>
                      )}
                      <div className="flex items-center gap-1">
                        <span>Workflow Run:</span>
                        <AppLink
                          href={`/workflows`}
                          className="inline-flex items-center gap-0.5 text-brand hover:underline"
                        >
                          {entry.result.workflow_run_id.slice(0, 8)}…
                          <ExternalLink className="size-3" />
                        </AppLink>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Input */}
      <div className="border-t px-6 py-4">
        <div className="flex items-end gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Tell the CEO what to do…"
            rows={1}
            className="flex-1 resize-none rounded-lg border bg-background px-3 py-2.5 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
            style={{ minHeight: 42, maxHeight: 160 }}
            onInput={(e) => {
              const target = e.target as HTMLTextAreaElement;
              target.style.height = "auto";
              target.style.height = `${Math.min(target.scrollHeight, 160)}px`;
            }}
          />
          <Button
            size="icon"
            disabled={!input.trim() || isSending}
            onClick={handleSend}
            className="shrink-0"
          >
            {isSending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Send className="size-4" />
            )}
          </Button>
        </div>
        <p className="mt-1.5 text-[11px] text-muted-foreground">
          Press Enter to send · Shift+Enter for new line
        </p>
      </div>
    </div>
  );
}
