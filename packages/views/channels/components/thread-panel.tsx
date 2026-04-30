"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import {
  channelMessageThreadOptions,
  useSendChannelMessage,
} from "@multica/core/channels";
import { Button } from "@multica/ui/components/ui/button";
import { X } from "lucide-react";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { MessageRow } from "./message-row";
import { useRef } from "react";

interface ThreadPanelProps {
  channelId: string;
  parentMessageId: string;
  onClose: () => void;
  enabled: boolean;
}

/**
 * ThreadPanel renders a single thread (parent + replies) in a side
 * column to the right of the main timeline. Sending in here posts
 * with parent_message_id set, so replies live under the parent rather
 * than landing in the top-level view.
 *
 * Width: 380px is conservative — narrow enough that the main timeline
 * stays readable on a 1280px window, wide enough for code blocks not
 * to wrap awkwardly. A future iteration could make this draggable.
 */
export function ThreadPanel({ channelId, parentMessageId, onClose, enabled }: ThreadPanelProps) {
  const wsId = useWorkspaceId();
  const { data: thread, isLoading } = useQuery(
    channelMessageThreadOptions(channelId, parentMessageId, enabled),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const memberById = new Map(members.map((m) => [m.user_id, m]));
  const agentById = new Map(agents.map((a) => [a.id, a]));

  const sendMut = useSendChannelMessage(channelId);
  const editorRef = useRef<ContentEditorRef>(null);
  const [isEmpty, setIsEmpty] = useState(true);

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    if (!content || sendMut.isPending) return;
    sendMut.mutate({ content, parent_message_id: parentMessageId });
    editorRef.current?.clearContent();
    setIsEmpty(true);
  };

  return (
    <aside className="flex w-[380px] shrink-0 flex-col border-l border-border bg-background">
      <header className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="text-sm font-semibold text-foreground">Thread</span>
        <Button
          size="sm"
          variant="ghost"
          onClick={onClose}
          aria-label="Close thread"
          className="h-6 w-6 p-0"
        >
          <X className="h-4 w-4" />
        </Button>
      </header>
      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <div className="px-4 py-6 text-sm text-muted-foreground">Loading thread…</div>
        ) : !thread ? (
          <div className="px-4 py-6 text-sm text-muted-foreground">Thread not found.</div>
        ) : (
          <>
            <div className="border-b border-border bg-muted/20">
              <MessageRow
                message={thread.parent}
                channelId={channelId}
                member={
                  thread.parent.author_type === "member"
                    ? memberById.get(thread.parent.author_id)
                    : undefined
                }
                agent={
                  thread.parent.author_type === "agent"
                    ? agentById.get(thread.parent.author_id)
                    : undefined
                }
                disableReplyAction
              />
              <div className="px-4 py-2 text-xs text-muted-foreground">
                {thread.replies.length === 0
                  ? "No replies yet."
                  : `${thread.replies.length} ${thread.replies.length === 1 ? "reply" : "replies"}`}
              </div>
            </div>
            {thread.replies.map((m) => (
              <MessageRow
                key={m.id}
                message={m}
                channelId={channelId}
                member={
                  m.author_type === "member" ? memberById.get(m.author_id) : undefined
                }
                agent={m.author_type === "agent" ? agentById.get(m.author_id) : undefined}
                disableReplyAction
              />
            ))}
          </>
        )}
      </div>
      <div className="border-t border-border bg-background px-4 py-3">
        <div className="rounded-md border border-input bg-background px-3 py-2 focus-within:ring-2 focus-within:ring-ring">
          <ContentEditor
            ref={editorRef}
            placeholder="Reply in thread…"
            onUpdate={(md) => setIsEmpty(!md.trim())}
            submitOnEnter
            onSubmit={handleSend}
          />
        </div>
        <div className="mt-2 flex justify-end">
          <Button size="sm" disabled={isEmpty || sendMut.isPending} onClick={handleSend}>
            {sendMut.isPending ? "Sending…" : "Reply"}
          </Button>
        </div>
      </div>
    </aside>
  );
}
