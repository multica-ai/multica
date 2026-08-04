// FIR-4350 — read/write the per-user Chat page display settings. Same storage
// and optimistic-write pattern as useChatPlacement: the server-synced
// user.preferences blob (PATCH /api/me/preferences via setUser), so the layout
// follows the user across devices with no extra table or endpoint.
"use client";

import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  CHAT_PAGE_SETTINGS_KEY,
  readChatPageSettings,
  type ChatPageSettings,
} from "./chat-page-settings";

export interface UseChatPageSettingsResult {
  settings: ChatPageSettings;
  /** Merge a partial change and persist it. */
  setSettings: (patch: Partial<ChatPageSettings>) => void;
}

export function useChatPageSettings(): UseChatPageSettingsResult {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);

  const prefs = user?.preferences as Record<string, unknown> | null | undefined;
  const saved = useMemo(
    () => readChatPageSettings(prefs?.[CHAT_PAGE_SETTINGS_KEY]),
    [prefs],
  );

  // Optimistic mirror so a change reflects instantly; rolled back to server
  // truth on a failed write.
  const [optimistic, setOptimistic] = useState<ChatPageSettings | null>(null);
  const settings = optimistic ?? saved;

  const setSettings = useCallback(
    (patch: Partial<ChatPageSettings>) => {
      const updated = { ...settings, ...patch };
      setOptimistic(updated);
      void (async () => {
        try {
          const savedUser = await api.updateMyPreferences({
            [CHAT_PAGE_SETTINGS_KEY]: updated,
          });
          setUser(savedUser);
        } catch (err) {
          toast.error(
            err instanceof Error ? err.message : "Failed to save chat settings",
          );
        } finally {
          setOptimistic(null);
        }
      })();
    },
    [settings, setUser],
  );

  return { settings, setSettings };
}
