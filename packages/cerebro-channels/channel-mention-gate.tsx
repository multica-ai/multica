"use client";

// FIR-2680: a one-step confirm shown when the user is about to send a channel
// message that @mentions someone who is not a participant. Mirrors the notes
// give-access prompt (packages/cerebro-notes note-comments-panel). The confirm
// only GATES the send — on "Add & send" the missing people are subscribed to
// the channel first (so the server guard then notifies them); on "Send without"
// the message posts as-is (the server drops the notification → a dead @link);
// on "Cancel" the draft stays. DMs never use this gate.

import { useCallback, useRef, useState } from "react";
import type { ReactNode } from "react";
import { UserPlus } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { useToggleChannelParticipant } from "@multica/core/channels";
import { useActorName } from "@multica/core/workspace/hooks";
import type { ChannelMember } from "@multica/core/types";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

interface PendingConfirm {
  missing: ChannelMember[];
  resolve: (proceed: boolean) => void;
}

export interface ChannelMentionGate {
  /**
   * Wire to the composer's `confirmBeforeSend`. Resolves `true` when the send
   * may proceed, `false` when the user cancels. When the flag is off, there are
   * no mentions, or every mentioned person is already a participant, it resolves
   * `true` synchronously — safe to await on every send.
   */
  confirmBeforeSend: (markdown: string) => Promise<boolean>;
  /** Render once inside the composer — the dialog portals itself. */
  confirmDialog: ReactNode;
}

// Match both member and agent mention links; a channel participant can be an
// agent as well as a member.
const MENTION_RE = /mention:\/\/(member|agent)\/([0-9a-fA-F-]{36})/g;

function extractMentions(markdown: string): ChannelMember[] {
  const seen = new Map<string, ChannelMember>();
  for (const m of markdown.matchAll(MENTION_RE)) {
    const user_type = m[1] as ChannelMember["user_type"];
    const user_id = m[2];
    if (user_id) seen.set(`${user_type}:${user_id}`, { user_type, user_id });
  }
  return [...seen.values()];
}

/**
 * useChannelMentionGate — gates a channel composer's send with an "add them to
 * this channel?" prompt when the draft @mentions a non-participant. Gated by the
 * `cerebro_channel_mention_members_only` flag so it flips together with the
 * server guard (Part A). `participants` is the channel's already-loaded member
 * list, so membership is decided client-side with no extra request.
 */
export function useChannelMentionGate(
  channelId: string,
  participants: ChannelMember[],
): ChannelMentionGate {
  const flagOn = useFeatureFlag("cerebro_channel_mention_members_only");
  const { getActorName } = useActorName();
  const toggle = useToggleChannelParticipant();
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const pendingRef = useRef<PendingConfirm | null>(null);

  const settle = useCallback((proceed: boolean) => {
    const current = pendingRef.current;
    pendingRef.current = null;
    setPending(null);
    current?.resolve(proceed);
  }, []);

  const confirmBeforeSend = useCallback(
    (markdown: string): Promise<boolean> => {
      if (!flagOn) return Promise.resolve(true);
      const mentioned = extractMentions(markdown);
      if (mentioned.length === 0) return Promise.resolve(true);
      const missing = mentioned.filter(
        (mm) =>
          !participants.some(
            (p) => p.user_id === mm.user_id && p.user_type === mm.user_type,
          ),
      );
      if (missing.length === 0) return Promise.resolve(true);
      return new Promise<boolean>((resolve) => {
        const next = { missing, resolve };
        pendingRef.current = next;
        setPending(next);
      });
    },
    [flagOn, participants],
  );

  const addAndSend = useCallback(async () => {
    const current = pendingRef.current;
    if (!current) return;
    pendingRef.current = null;
    setPending(null);
    let anyFailed = false;
    await Promise.all(
      current.missing.map(
        (member) =>
          new Promise<void>((res) => {
            toggle.mutate(
              { channelId, member, action: "add" },
              {
                onError: () => {
                  anyFailed = true;
                  res();
                },
                onSuccess: () => res(),
              },
            );
          }),
      ),
    );
    if (anyFailed) toast.error("Couldn't add everyone — sending anyway.");
    current.resolve(true);
  }, [channelId, toggle]);

  const missing = pending?.missing ?? [];
  const names = missing.map((mm) => getActorName(mm.user_type, mm.user_id));
  const namesText = names.join(", ");
  const isPlural = missing.length > 1;

  const confirmDialog = (
    <AlertDialog
      open={pending !== null}
      onOpenChange={(open) => {
        if (!open) settle(false);
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogMedia>
            <UserPlus />
          </AlertDialogMedia>
          <AlertDialogTitle>Add to this channel?</AlertDialogTitle>
          <AlertDialogDescription>
            {`${namesText} ${isPlural ? "are" : "is"} not in this channel, so they won't get your message. Add them to the channel, or send without notifying them.`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <Button variant="ghost" onClick={() => settle(false)}>
            Cancel
          </Button>
          <Button variant="outline" onClick={() => settle(true)}>
            Send without
          </Button>
          <Button onClick={addAndSend}>Add &amp; send</Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return { confirmBeforeSend, confirmDialog };
}
