"use client";

import { useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import { useChannelsStore, useSendChannelMessage, channelMembersOptions } from "@multica/core/channels";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Paperclip, Send } from "lucide-react";
import type { Channel } from "@multica/core/types";
import {
  PendingAttachmentsRow,
  type PendingAttachment,
} from "./message-attachments";
import { toast } from "sonner";

interface ChannelComposerProps {
  channel: Channel;
  disabled?: boolean;
}

/**
 * Build the composer placeholder for a channel. For regular channels
 * the format is `Message #<display>`; for DMs the deterministic hash
 * (channel.name = "dm-<sha256>") is meaningless to humans, so we
 * resolve the *other* participant's display name from membership and
 * render `Message <name>` (no `#`). Self-DMs read as "Message yourself".
 *
 * Falls back to a sane label while membership is loading so the
 * placeholder never flashes the raw hash.
 */
function useComposerPlaceholder(channel: Channel): string {
  const wsId = useWorkspaceId();
  const selfId = useAuthStore((s) => s.user?.id ?? null);
  const isDM = channel.kind === "dm";
  const { data: members = [] } = useQuery(channelMembersOptions(channel.id, isDM));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceAgents = [] } = useQuery(agentListOptions(wsId));

  return useMemo(() => {
    if (!isDM) {
      const name = channel.display_name || channel.name;
      return `Message #${name}`;
    }
    if (members.length === 0) {
      return "Message"; // membership still loading
    }
    const others = members.filter(
      (m) => !(m.member_type === "member" && m.member_id === selfId),
    );
    if (others.length === 0) {
      return "Message yourself"; // self-DM
    }
    const o = others[0]!;
    if (o.member_type === "agent") {
      const a = workspaceAgents.find((x) => x.id === o.member_id);
      return `Message ${a?.name ?? "agent"}`;
    }
    const wm = workspaceMembers.find((m) => m.user_id === o.member_id);
    return `Message ${wm?.name || wm?.email || "teammate"}`;
  }, [isDM, channel.display_name, channel.name, members, selfId, workspaceMembers, workspaceAgents]);
}

/**
 * ChannelComposer is the bottom-of-screen input. It reuses the shared
 * ContentEditor so we get markdown, mentions (@member, @agent), styling,
 * and file-drop affordances for free.
 *
 * Drafts persist per-channel via the channels store — switching channels
 * preserves whatever you were typing.
 *
 * Submit is wired to Enter (Shift+Enter for newline) via the editor's
 * submitOnEnter prop.
 */
export function ChannelComposer({ channel, disabled }: ChannelComposerProps) {
  const editorRef = useRef<ContentEditorRef>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const inputDraft = useChannelsStore((s) => s.inputDrafts[channel.id] ?? "");
  const setInputDraft = useChannelsStore((s) => s.setInputDraft);
  const clearInputDraft = useChannelsStore((s) => s.clearInputDraft);
  const sendMut = useSendChannelMessage(channel.id);
  const [isEmpty, setIsEmpty] = useState(!inputDraft.trim());
  const [pending, setPending] = useState<PendingAttachment[]>([]);
  const placeholder = useComposerPlaceholder(channel);

  // Block send while any attachment is still uploading. The send button
  // reflects this so the user understands why it's grayed out.
  const hasUploading = pending.some((p) => p.status === "uploading");
  const hasError = pending.some((p) => p.status === "error");

  const handleAttach = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    // Each file gets a stable client key + an "uploading" placeholder so
    // the chip row appears immediately. Uploads run in parallel.
    const additions: PendingAttachment[] = Array.from(files).map((f, i) => ({
      key: `${Date.now()}-${i}-${f.name}`,
      filename: f.name,
      contentType: f.type,
      serverID: null,
      status: "uploading",
    }));
    setPending((prev) => [...prev, ...additions]);

    await Promise.all(
      additions.map(async (p, i) => {
        try {
          const att = await api.uploadFile(files[i]!);
          setPending((prev) =>
            prev.map((x) =>
              x.key === p.key ? { ...x, serverID: att.id, status: "ready" } : x,
            ),
          );
        } catch (err) {
          const msg = err instanceof Error ? err.message : "upload failed";
          setPending((prev) =>
            prev.map((x) => (x.key === p.key ? { ...x, status: "error", error: msg } : x)),
          );
          toast.error(`${p.filename}: ${msg}`);
        }
      }),
    );
  };

  const removePending = (key: string) =>
    setPending((prev) => prev.filter((x) => x.key !== key));

  const handleSend = () => {
    const content = editorRef.current?.getMarkdown()?.replace(/(\n\s*)+$/, "").trim();
    const readyAttachmentIDs = pending
      .filter((p) => p.status === "ready" && p.serverID)
      .map((p) => p.serverID!);
    // Allow attachment-only messages (whitespace content, ≥1 ready file) — Slack-like.
    const hasAnyContent = !!content || readyAttachmentIDs.length > 0;
    if (!hasAnyContent || hasUploading || disabled || sendMut.isPending) return;
    sendMut.mutate({
      content: content ?? "",
      ...(readyAttachmentIDs.length > 0 ? { attachment_ids: readyAttachmentIDs } : {}),
    });
    editorRef.current?.clearContent();
    clearInputDraft(channel.id);
    setIsEmpty(true);
    setPending([]);
  };

  // The dashboard layout mounts a Chat FAB at `absolute bottom-2 right-2 z-50`
  // (size-10 = 40px, plus its 8px offset → it owns roughly the
  // bottom-right 56px square). Padding the composer's right edge by
  // 14 (56px) on md+ keeps the Send button clear of the FAB instead of
  // getting half-clipped underneath it.
  // Send is enabled when EITHER the body has text OR ≥1 attachment is
  // ready. Disabling on hasError keeps the user from sending a message
  // that's missing a file they meant to include — they need to retry
  // or remove the failing chip first.
  const sendDisabled =
    disabled ||
    sendMut.isPending ||
    hasUploading ||
    hasError ||
    (isEmpty && pending.filter((p) => p.status === "ready").length === 0);

  return (
    <div className="border-t border-border bg-background px-4 py-3 md:pr-14">
      <PendingAttachmentsRow pending={pending} onRemove={removePending} />
      <div className="flex items-end gap-2">
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            void handleAttach(e.target.files);
            // Reset so the same file can be re-selected after removal.
            e.target.value = "";
          }}
          aria-hidden="true"
        />
        <Button
          size="sm"
          variant="ghost"
          onClick={() => fileInputRef.current?.click()}
          disabled={disabled}
          aria-label="Attach file"
          className="h-9 w-9 p-0"
        >
          <Paperclip className="h-4 w-4" />
        </Button>
        <div
          className={[
            "flex-1 rounded-md border border-input bg-background px-3 py-2 focus-within:ring-2 focus-within:ring-ring",
            disabled ? "pointer-events-none opacity-60" : "",
          ].join(" ")}
          aria-disabled={disabled || undefined}
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={inputDraft}
            onUpdate={(md) => {
              setInputDraft(channel.id, md);
              setIsEmpty(!md.trim());
            }}
            placeholder={placeholder}
            submitOnEnter
            onSubmit={handleSend}
          />
        </div>
        <Button
          size="sm"
          onClick={handleSend}
          disabled={sendDisabled}
          aria-label="Send message"
        >
          <Send className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
