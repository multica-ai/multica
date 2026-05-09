"use client";
import React, { useState, useRef, useEffect, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSendChannelMessage, channelMembersOptions } from "@multica/core/channels";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ChannelMember } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { Bot, User } from "lucide-react";

interface MentionCandidate {
  id: string;
  type: "user" | "agent";
  name: string;
}

/**
 * Parse the current @mention query from the text up to the cursor position.
 * Returns null when the cursor is not inside a mention token.
 */
function parseMentionQuery(
  text: string,
  cursor: number,
): { query: string; start: number } | null {
  let i = cursor - 1;
  while (i >= 0 && text[i] !== "@" && text[i] !== " " && text[i] !== "\n") {
    i--;
  }
  if (i < 0 || text[i] !== "@") return null;
  const query = text.slice(i + 1, cursor);
  if (query.includes(" ")) return null;
  return { query, start: i };
}

export function ChannelComposer({ channelId }: { channelId: string }) {
  const wsId = useWorkspaceId();
  const [content, setContent] = useState("");
  const [mentionQuery, setMentionQuery] = useState<string | null>(null);
  const [mentionStart, setMentionStart] = useState(0);
  const [selectedIdx, setSelectedIdx] = useState(0);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const sendMessage = useSendChannelMessage(wsId, channelId);

  const { data: members = [] } = useQuery(channelMembersOptions(wsId, channelId));

  // Build filtered candidate list from channel members
  const allCandidates: MentionCandidate[] = (members as ChannelMember[]).map((m) => ({
    id: m.member_id,
    type: m.member_type,
    name: m.name || m.member_id,
  }));

  const candidates = mentionQuery === null
    ? allCandidates
    : allCandidates.filter((c) =>
        c.name.toLowerCase().includes(mentionQuery.toLowerCase()),
      );

  useEffect(() => {
    setSelectedIdx(0);
  }, [mentionQuery]);

  const insertMention = useCallback(
    (candidate: MentionCandidate) => {
      const cursorEnd = textareaRef.current?.selectionEnd ?? mentionStart;
      const before = content.slice(0, mentionStart);
      const after = content.slice(cursorEnd);
      const token = `@${candidate.name} `;
      const next = before + token + after;
      setContent(next);
      setMentionQuery(null);
      requestAnimationFrame(() => {
        const pos = before.length + token.length;
        textareaRef.current?.setSelectionRange(pos, pos);
        textareaRef.current?.focus();
      });
    },
    [content, mentionStart],
  );

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const v = e.target.value;
    setContent(v);
    const cursor = e.target.selectionStart ?? v.length;
    const parsed = parseMentionQuery(v, cursor);
    if (parsed) {
      setMentionQuery(parsed.query);
      setMentionStart(parsed.start);
    } else {
      setMentionQuery(null);
    }
  };

  const handleSend = async () => {
    if (!content.trim()) return;
    await sendMessage.mutateAsync({ content: content.trim() });
    setContent("");
    setMentionQuery(null);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const showDropdown = mentionQuery !== null && candidates.length > 0;
    if (showDropdown) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSelectedIdx((idx) => (idx + 1) % candidates.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSelectedIdx((idx) => (idx - 1 + candidates.length) % candidates.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        const c = candidates[selectedIdx];
        if (c) insertMention(c);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMentionQuery(null);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const showDropdown = mentionQuery !== null && candidates.length > 0;

  return (
    <div className="border-t p-3">
      {showDropdown && (
        <div className="mb-2 rounded-md border bg-popover shadow-md overflow-hidden">
          <div className="px-2 py-1 text-xs text-muted-foreground border-b">提及成员</div>
          {candidates.map((c, i) => (
            <button
              key={c.id}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault();
                insertMention(c);
              }}
              className={cn(
                "flex w-full items-center gap-2 px-3 py-1.5 text-sm transition-colors",
                i === selectedIdx ? "bg-accent text-accent-foreground" : "hover:bg-muted",
              )}
            >
              {c.type === "agent" ? (
                <Bot className="size-3.5 shrink-0 text-purple-500" />
              ) : (
                <User className="size-3.5 shrink-0 text-muted-foreground" />
              )}
              <span className="flex-1 text-left truncate">{c.name}</span>
              <span className="text-xs text-muted-foreground">
                {c.type === "agent" ? "Agent" : "用户"}
              </span>
            </button>
          ))}
        </div>
      )}

      <div className="flex gap-2 items-end">
        <div className="flex-1">
          <textarea
            ref={textareaRef}
            className="w-full min-h-[40px] max-h-[120px] rounded-md border bg-background px-3 py-2 text-sm resize-none focus:outline-none focus:ring-1 focus:ring-ring"
            placeholder="输入消息... (Enter 发送, Shift+Enter 换行, @ 提及成员)"
            value={content}
            onChange={handleChange}
            onKeyDown={handleKeyDown}
            disabled={sendMessage.isPending}
          />
        </div>
        <Button
          size="sm"
          onClick={handleSend}
          disabled={!content.trim() || sendMessage.isPending}
          className="shrink-0"
        >
          发送
        </Button>
      </div>
    </div>
  );
}
