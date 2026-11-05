import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { chatDraftRestoresOptions } from "@multica/core/chat/queries";
import { useConsumeChatDraftRestore } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { removeChatMessageFromCaches } from "@multica/core/realtime";
import type { Attachment } from "@multica/core/types";

/**
 * A draft the composer is asked to adopt. Two sources feed it:
 * - a failed send, restoring the user's own text (no `serverRestoreId`);
 * - a durable deferred-cancellation restore (#5219), which has a server row.
 */
export interface RestoreDraftRequest {
  id: string;
  content: string;
  attachments?: Attachment[];
  /**
   * Draft slot this restore targets. The composer only applies it while the
   * user is viewing that session, so a restore never lands in the wrong chat.
   */
  sessionId?: string;
  /** Set for durable restores: the row to delete once the draft is applied. */
  serverRestoreId?: string;
}

/**
 * Owns draft restoration for the chat page and floating composer.
 * Durable cancellation restores remain on the server until applied. Failed-send
 * and synchronous-cancel restores have no server copy, so persist them per session
 * until the composer confirms application. The shared request slot exposes only
 * the visible session's next offer.
 *
 * Only visible composers participate. A nonempty draft defers the offer without
 * consuming it. Record application in a persisted ledger before sending consume;
 * retry consumption on later fetches and clear the ledger on server confirmation.
 * This ordering prevents a lost response from offering an already-applied prompt.
 *
 * Two visible clients may restore the same prompt locally; consumption is
 * idempotent. Apply before consume: claiming first could lose the only copy if
 * the winning client closes before writing its draft.
 */
export function useChatDraftRestore(activeSessionId: string | null, enabled = true) {
  const qc = useQueryClient();
  const [restoreDraftRequest, setRestoreDraftRequest] = useState<RestoreDraftRequest | null>(null);

  const appliedRestoreIds = useChatStore((s) => s.appliedDraftRestoreIds);
  const markDraftRestoreApplied = useChatStore((s) => s.markDraftRestoreApplied);
  const forgetDraftRestoreApplied = useChatStore((s) => s.forgetDraftRestoreApplied);
  const pendingSendRestores = useChatStore((s) => s.pendingSendRestores);
  const enqueuePendingSendRestore = useChatStore((s) => s.enqueuePendingSendRestore);
  const dequeuePendingSendRestore = useChatStore((s) => s.dequeuePendingSendRestore);
  const consumeDraftRestore = useConsumeChatDraftRestore();

  // The query refetches on composer mount and on network reconnect, and the
  // initiator's realtime handler invalidates it when chat:cancel_finalized
  // arrives — so a client that was offline across the broadcast still recovers
  // the prompt here. Disabled composers do not even fetch: no offer, no claim.
  const { data: draftRestoresData } = useQuery({
    ...chatDraftRestoresOptions(activeSessionId ?? ""),
    enabled: enabled && !!activeSessionId,
  });
  const restores = draftRestoresData?.restores;

  // In-flight consumes, so a re-render or a second fetch doesn't fire a duplicate
  // request for the same row. Released on settle — including on failure, which is
  // what lets the next fetch reconcile a consume whose retries all ran out.
  const reconcilingRef = useRef<Set<string>>(new Set());
  const consume = useCallback(
    (sessionId: string, restoreId: string) => {
      reconcilingRef.current.add(restoreId);
      consumeDraftRestore.mutate(
        { sessionId, restoreId },
        {
          // The row is gone: the ledger entry has nothing left to protect against.
          onSuccess: () => forgetDraftRestoreApplied(restoreId),
          onSettled: () => reconcilingRef.current.delete(restoreId),
        },
      );
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mutation ref stable
    [forgetDraftRestoreApplied],
  );

  // Reconcile rows whose consume never landed. Firing this from the fetched list
  // (rather than only at apply time) is what closes the lost-consume hole: the
  // row is re-consumed on the next mount, refetch or reconnect until it is gone.
  useEffect(() => {
    if (!enabled || !activeSessionId || !restores) return;
    for (const r of restores) {
      if (r.chat_session_id !== activeSessionId) continue;
      if (!appliedRestoreIds.includes(r.id)) continue;
      if (reconcilingRef.current.has(r.id)) continue;
      consume(activeSessionId, r.id);
    }
  }, [restores, activeSessionId, appliedRestoreIds, consume, enabled]);

  useEffect(() => {
    if (!enabled) {
      // The composer went away (window closed, tab backgrounded). Release the
      // slot rather than hold an offer nobody can see: a durable request is
      // refetched, a server-less one is still in its persisted queue. Nothing was
      // applied, so nothing is lost, and the composer the user IS looking at —
      // here or on another device — gets to claim it.
      if (restoreDraftRequest) setRestoreDraftRequest(null);
      return;
    }
    if (!activeSessionId) return;
    if (restoreDraftRequest) {
      // One rule for the single slot: it only ever holds a request for the
      // session on screen. ChatInput treats a request it cannot apply as a WAIT,
      // so one aimed at a session the user may never return to would otherwise
      // starve every hand-off for the session they ARE looking at.
      //
      // Releasing it costs nothing either way: a durable request has a server row
      // and is offered again on the next fetch; a server-less one is still in
      // pendingSendRestores, queued against its own session, and is re-offered
      // when the user comes back to it.
      const { sessionId } = restoreDraftRequest;
      if (!sessionId || sessionId === activeSessionId) return;
      setRestoreDraftRequest(null);
      return;
    }
    // Server-less restores go first. Both kinds are re-offered until applied, but
    // this one is the user's own text with no copy anywhere else, so it is the
    // one to get in front of them soonest. It stays in the queue until ChatInput
    // reports the hand-off — a request that is merely offered can still be lost.
    const queued = pendingSendRestores[activeSessionId]?.[0];
    if (queued) {
      setRestoreDraftRequest({
        id: queued.id,
        content: queued.content,
        attachments: queued.attachments,
        sessionId: activeSessionId,
      });
      return;
    }
    const restore = restores?.find(
      (r) => r.chat_session_id === activeSessionId && !appliedRestoreIds.includes(r.id),
    );
    if (!restore) return;
    // If this client missed the chat:cancel_finalized event, the deleted bubble
    // may still sit in the message caches — drop it before restoring.
    removeChatMessageFromCaches(qc, activeSessionId, restore.id);
    setRestoreDraftRequest({
      id: restore.id,
      content: restore.content ?? "",
      attachments: restore.attachments,
      sessionId: activeSessionId,
      serverRestoreId: restore.id,
    });
  }, [
    restores,
    activeSessionId,
    restoreDraftRequest,
    appliedRestoreIds,
    pendingSendRestores,
    qc,
    enabled,
  ]);

  /**
   * A restore with no server copy — a failed send, or a cancel that answered
   * synchronously. It goes straight into the persisted per-session queue, never
   * into the shared slot: its text exists nowhere else, so it has to survive an
   * unmount, and it must not hold the slot hostage for a session the user is not
   * looking at. The effect above hands it to the composer when they are.
   */
  const enqueueLocalRestore = useCallback(
    (restore: { id: string; content: string; attachments?: Attachment[]; sessionId: string }) => {
      enqueuePendingSendRestore(restore);
    },
    [enqueuePendingSendRestore],
  );

  /**
   * The composer wrote the draft. This is the only terminal transition: for a
   * durable restore, record it in the ledger before consuming; for a server-less
   * one, drop the queue entry now that its text lives in the (persisted) draft.
   * A skipped hand-off never reaches here.
   */
  const handleRestoreDraftApplied = useCallback(() => {
    if (!restoreDraftRequest) return;
    const { id, serverRestoreId, sessionId } = restoreDraftRequest;
    if (serverRestoreId && sessionId) {
      markDraftRestoreApplied(serverRestoreId);
      consume(sessionId, serverRestoreId);
    } else if (sessionId) {
      dequeuePendingSendRestore(sessionId, id);
    }
    setRestoreDraftRequest(null);
  }, [restoreDraftRequest, markDraftRestoreApplied, consume, dequeuePendingSendRestore]);

  return { restoreDraftRequest, enqueueLocalRestore, handleRestoreDraftApplied };
}
