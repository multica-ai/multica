// FIR-4350 — read/write the per-user conversation placement. Same storage as
// cerebro_inbox_favorites: the server-synced user.preferences blob (PATCH
// /api/me/preferences via setUser), so placement follows the user across
// devices with no extra table or endpoint.
"use client";

import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  CHAT_PLACEMENT_KEY,
  readPlacement,
  setPlace,
  type ChatPlacement,
  type ConversationKind,
  type PlacementEntry,
} from "./chat-placement";

export interface UseChatPlacementResult {
  placement: ChatPlacement;
  /** Toggle one place for one type. Refused when it is the type's last place. */
  setPlacement: (
    kind: ConversationKind,
    place: keyof PlacementEntry,
    next: boolean,
  ) => void;
}

export function useChatPlacement(): UseChatPlacementResult {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);

  const prefs = user?.preferences as Record<string, unknown> | null | undefined;
  const saved = useMemo(() => readPlacement(prefs?.[CHAT_PLACEMENT_KEY]), [prefs]);

  // Optimistic mirror so a toggle flips instantly; rolled back to server truth
  // on a failed write.
  const [optimistic, setOptimistic] = useState<ChatPlacement | null>(null);
  const placement = optimistic ?? saved;

  const setPlacement = useCallback(
    (kind: ConversationKind, place: keyof PlacementEntry, next: boolean) => {
      const updated = setPlace(placement, kind, place, next);
      if (updated === placement) return; // refused — would leave the type nowhere
      setOptimistic(updated);
      void (async () => {
        try {
          const saved = await api.updateMyPreferences({ [CHAT_PLACEMENT_KEY]: updated });
          setUser(saved);
        } catch (err) {
          // Roll back to server truth and tell the user — otherwise the switch
          // silently springs back and they think the change stuck.
          toast.error(
            err instanceof Error ? err.message : "Failed to save chat placement",
          );
        } finally {
          setOptimistic(null);
        }
      })();
    },
    [placement, setUser],
  );

  return { placement, setPlacement };
}
