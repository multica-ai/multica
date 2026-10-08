"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  agentBuilderSessionKeys,
  decodeBuilderInput,
  pickBuilderRestore,
  type BuilderRestore,
} from "@multica/core/agents";
import { api, ApiError } from "@multica/core/api";
import {
  chatKeys,
  chatMessagesOptions,
  pendingChatTaskOptions,
} from "@multica/core/chat/queries";
import { upsertChatMessageToCaches } from "@multica/core/chat/message-cache";
import { removeChatMessageFromCaches } from "@multica/core/realtime";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ChatMessage } from "@multica/core/types";
import { useAppForeground } from "../../common/use-app-foreground";
import { useChatDraftRestore } from "../../chat/components/use-chat-draft-restore";
import { useT } from "../../i18n";

const EMPTY_CHAT_MESSAGES: ChatMessage[] = [];

/**
 * Lifecycle of one AI-builder conversation: create the hidden carrier session,
 * exchange messages on it, rebind its runtime, and destroy it on request.
 *
 * The conversation is identified by `sessionId`, which the page reads off its
 * own URL. That is what makes it survive a refresh, a back/forward, and a
 * reopened tab — the hook holds no session state of its own, so unmounting it
 * (a sidebar click, a route change) leaves the conversation intact on the
 * server. Destroying one is now an explicit act, never a side effect of
 * navigation.
 *
 * There is no polling: the global realtime sync already invalidates
 * `chatKeys.messages` / `chatKeys.pendingTask` per session id on
 * `chat:message`, `chat:done` and the task lifecycle events, exactly as it does
 * for the main chat window — which has never polled.
 */
export function useBuilderSession(options: {
  /** The conversation to operate on; empty string before one is started. */
  sessionId: string;
  /** Encodes composer text into the builder wire format for the current draft. */
  encodeInput: (text: string) => string;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { sessionId } = options;

  // Anything that changes whether this conversation is listable, or what its
  // row says, has to reach the drafts list — it is rendered on other screens
  // that are already mounted or will mount from cache.
  const invalidateDraftList = () =>
    qc.invalidateQueries({ queryKey: agentBuilderSessionKeys.list(wsId) });

  const [starting, setStarting] = useState(false);
  const [closing, setClosing] = useState(false);
  const [switchingRuntime, setSwitchingRuntime] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [restoreDraft, setRestoreDraft] = useState<BuilderRestore | null>(null);

  // The builder chat is a real chat_session, so cancelling a started-but-empty
  // run defers the empty/non-empty judgment exactly as it does in the main chat
  // (#5219): stop's response carries no restore_to_input, and the prompt arrives
  // later as a durable chat_draft_restore row. Without this hook the studio
  // composer would simply never see it. The two sources are exclusive — the
  // synchronous cancel answers immediately, the durable one lands after the
  // daemon acks — so whichever exists is handed to the composer.
  //
  // Gated on app foreground: this is a dedicated route, so being mounted means
  // the surface is on screen, but a backgrounded tab must not fetch/apply/
  // consume a restore the user is waiting on elsewhere. It recovers on its next
  // fetch once the tab is refocused.
  const appForeground = useAppForeground();
  const {
    restoreDraftRequest: durableRestoreRequest,
    handleRestoreDraftApplied: handleDurableRestoreApplied,
  } = useChatDraftRestore(sessionId || null, appForeground);
  const restoreDraftRequest = useMemo(
    () => pickBuilderRestore(restoreDraft, durableRestoreRequest),
    [restoreDraft, durableRestoreRequest],
  );

  const messagesQuery = useQuery(chatMessagesOptions(sessionId));
  const pendingQuery = useQuery(pendingChatTaskOptions(sessionId));
  const messages = messagesQuery.data ?? EMPTY_CHAT_MESSAGES;
  const pending = !!pendingQuery.data?.task_id;
  // The conversation named by the URL is gone — discarded here, deleted from
  // another tab, or a link that outlived it. Read off the transcript fetch
  // rather than "absent from the drafts list", because a conversation is only
  // listed once it has a message: a session created a second ago is legitimately
  // missing from the list and must not be mistaken for a dead one.
  const missing =
    messagesQuery.error instanceof ApiError &&
    messagesQuery.error.status === 404;

  /** Creates the conversation. Returns its id so the caller can address it. */
  const start = async (
    runtimeId: string,
    model: string,
  ): Promise<string | null> => {
    setStarting(true);
    setError(null);
    try {
      const session = await api.createAgentBuilderSession({
        runtime_id: runtimeId,
        model: model.trim() || undefined,
      });
      if (!session.session_id) {
        throw new Error(t(($) => $.creation_studio.builder.start_failed));
      }
      return session.session_id;
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.creation_studio.builder.start_failed),
      );
      return null;
    } finally {
      setStarting(false);
    }
  };

  /**
   * Destroys the conversation. Returns false when the server refused, so the
   * caller can stay put and show the error instead of navigating away from a
   * conversation that still exists.
   */
  const destroy = async (): Promise<boolean> => {
    if (!sessionId) return true;
    setClosing(true);
    setError(null);
    try {
      await api.deleteChatSession(sessionId);
      qc.removeQueries({ queryKey: chatKeys.messages(sessionId) });
      // The send seeds both caches, so both must be dropped here. Leaving the
      // paged one behind would keep a deleted session's transcript sitting
      // fresh forever — it is staleTime: Infinity, so no reader self-corrects.
      qc.removeQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      qc.removeQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      void invalidateDraftList();
      return true;
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.creation_studio.builder.stop_failed),
      );
      return false;
    } finally {
      setClosing(false);
    }
  };

  /**
   * Retires the conversation once its agent exists.
   *
   * Archived, not deleted: the conversation is the record of how this agent was
   * designed, and an archived session is read-only and drops out of the drafts
   * list, which is exactly the desired end state. Deleting it destroyed that
   * record for no gain — the carrier costs nothing while idle, since usage is
   * booked per task and an archived session can never run one.
   *
   * Best-effort by design: the agent is already committed at this point, so a
   * failure here must never turn a successful create into a retryable error.
   */
  const archiveAfterCreate = async () => {
    if (!sessionId) return;
    try {
      await api.setChatSessionArchived(sessionId, true);
      void invalidateDraftList();
    } catch {
      // The draft stays listed; the user can discard it explicitly.
    }
  };

  // Rebinds the conversation's execution runtime on the server BEFORE the draft
  // reflects the new selection. Updating the draft first is what produced
  // MUL-5163: the picker showed runtime B while every subsequent message still
  // ran on the runtime the session was created with.
  const switchRuntime = async (runtimeId: string): Promise<string | null> => {
    if (switchingRuntime) return null;
    setSwitchingRuntime(true);
    setError(null);
    try {
      const result = await api.switchAgentBuilderRuntime(sessionId, {
        runtime_id: runtimeId,
      });
      toast.success(
        t(($) => $.creation_studio.builder.switch_runtime_success),
      );
      // Follow the runtime the server says it bound. Resolving here at all means
      // the rebind committed, so refusing to move the draft would leave the
      // picker pointing at a runtime that no longer executes anything — the same
      // split this whole path removes. The client fallback already resolves an
      // unparseable success body to the requested id.
      return result.runtime_id || runtimeId;
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.creation_studio.builder.switch_runtime_failed),
      );
      return null;
    } finally {
      setSwitchingRuntime(false);
    }
  };

  /**
   * Sends one turn.
   *
   * `commitInput` is the composer's clear (MUL-5181): it runs the moment the
   * server has accepted the message and the caches render it, NOT after the
   * reconciling invalidations settle. Awaiting those held the user's text in
   * the box for three more round-trips while their message was already on
   * screen. Same ordering as the main chat's handleSend.
   *
   * Optional because the empty-state prompt buttons call this directly, with
   * no composer to clear.
   */
  const send = async (
    content: string,
    commitInput?: () => void,
  ): Promise<boolean> => {
    const text = content.trim();
    // switchingRuntime blocks the send while a rebind is in flight: the server
    // serialises the two anyway, but letting the message through would mean the
    // user cannot tell which runtime answered it.
    if (!text || !sessionId || pending || switchingRuntime) return false;
    setError(null);
    try {
      const encodedContent = options.encodeInput(text);
      const result = await api.sendChatMessage(sessionId, encodedContent);
      const createdAt = new Date().toISOString();
      // Same door as the chat surfaces (MUL-5711). This path used to write the
      // flat cache only, so a Builder send left the paged cache — which the
      // chat surfaces read for the same session — without the message.
      upsertChatMessageToCaches(
        qc,
        sessionId,
        {
          id: result.message_id,
          chat_session_id: sessionId,
          role: "user",
          content: encodedContent,
          task_id: result.task_id,
          created_at: createdAt,
        },
        { seedIfMissing: true },
      );
      qc.setQueryData(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
        created_at: createdAt,
      });
      // Accepted and rendered — release the composer before reconciling.
      commitInput?.();
      void qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      // Both caches are seeded above, so both need the authoritative refetch —
      // otherwise a seeded one-message page could outlive the send as the whole
      // history a later reader sees.
      void qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      void qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      // The first turn is what makes a brand-new conversation listable at
      // all, and every later one moves it to the top with a new preview.
      void invalidateDraftList();
      return true;
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.creation_studio.builder.send_failed),
      );
      return false;
    }
  };

  const stop = async () => {
    const taskId = pendingQuery.data?.task_id;
    if (!taskId || !sessionId) return;
    qc.setQueryData(chatKeys.pendingTask(sessionId), {});
    try {
      const result = await api.cancelTaskById(taskId);
      const restored = result.cancelled_chat_message;
      if (restored) {
        // The server deleted this prompt on restore, so drop it from both
        // caches before reconciling — same order as the chat surfaces'
        // cancelChatTask. Without it the row lingers until the refetch lands,
        // and in the paged cache it would linger for good.
        removeChatMessageFromCaches(qc, restored.chat_session_id, restored.message_id);
        if (restored.restore_to_input) {
          setRestoreDraft({
            id: restored.message_id,
            content: decodeBuilderInput(restored.content),
          });
        }
      }
      await Promise.all([
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) }),
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) }),
        qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) }),
      ]);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : t(($) => $.creation_studio.builder.stop_failed),
      );
      // The cancel may still have landed server-side, so re-read the messages
      // too rather than only the pending marker.
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
    }
  };

  return {
    messages,
    missing,
    messagesLoading: messagesQuery.isLoading,
    pendingTask: pendingQuery.data,
    pending,
    starting,
    closing,
    switchingRuntime,
    error,
    restoreDraftRequest,
    handleRestoreDraftApplied: () => {
      if (restoreDraft) {
        setRestoreDraft(null);
        return;
      }
      handleDurableRestoreApplied();
    },
    start,
    destroy,
    archiveAfterCreate,
    switchRuntime,
    send,
    stop,
  };
}
