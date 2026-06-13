// TECH-3413 — read/write the dynamic-inbox layout from the server-synced
// user.preferences blob (PATCH /api/me/preferences via setUser), so a user's
// inbox setup follows them across devices. A separate key holds an optional
// mobile/PWA layout; when the viewport is mobile and a mobile layout exists, it
// wins, otherwise the desktop layout is used for both.
import { useCallback, useMemo, useState } from "react";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  DEFAULT_INBOX_LAYOUT,
  dedupeLayoutIds,
  cloneInboxLayout,
  deleteUserInboxPreset,
  isValidLayout,
  makeId,
  makeUserInboxPresetsBlob,
  readUserInboxPresets,
  upsertUserInboxPreset,
  type InboxLayout,
  type UserInboxPreset,
} from "./layout";

export const INBOX_LAYOUT_KEY = "cerebro_inbox_layout";
export const INBOX_LAYOUT_MOBILE_KEY = "cerebro_inbox_layout_mobile";
export const USER_INBOX_PRESETS_KEY = "cerebro_inbox_user_presets";

function readLayout(prefs: Record<string, unknown> | null | undefined, key: string): InboxLayout | null {
  const raw = prefs?.[key];
  return isValidLayout(raw) ? raw : null;
}

export interface UseInboxLayoutResult {
  layout: InboxLayout;
  /** True when the active layout is the mobile-specific one. */
  isMobileLayout: boolean;
  /** Persist a new layout to the active surface (desktop or mobile). */
  setLayout: (next: InboxLayout) => void;
  /** User-saved layout presets, synced through the preferences blob. */
  userPresets: UserInboxPreset[];
  /** Save the active layout as a named user preset. */
  saveUserPreset: (label: string) => void;
  /** Remove a user-saved preset. */
  deleteUserPreset: (presetId: string) => void;
  /** Whether this surface has a saved layout yet (vs. the built-in default). */
  hasSavedLayout: boolean;
}

export function useInboxLayout(): UseInboxLayoutResult {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const isMobile = useIsMobile();

  const prefs = user?.preferences as Record<string, unknown> | null | undefined;
  const desktopSaved = readLayout(prefs, INBOX_LAYOUT_KEY);
  const mobileSaved = readLayout(prefs, INBOX_LAYOUT_MOBILE_KEY);
  const userPresets = useMemo(
    () => readUserInboxPresets(prefs?.[USER_INBOX_PRESETS_KEY]),
    [prefs],
  );

  // Mobile uses its own layout only when one has been saved; otherwise it falls
  // back to the desktop layout so a user doesn't see an empty inbox on phone.
  const isMobileLayout = isMobile && !!mobileSaved;
  const activeKey = isMobileLayout ? INBOX_LAYOUT_MOBILE_KEY : INBOX_LAYOUT_KEY;
  const saved = isMobileLayout ? mobileSaved : desktopSaved;

  // Local optimistic mirror so reorders/edits feel instant; seeded from prefs.
  const fallback = useMemo(() => saved ?? DEFAULT_INBOX_LAYOUT(), [saved]);
  const [optimistic, setOptimistic] = useState<InboxLayout | null>(null);
  // TECH-3502 #1 — heal any colliding ids from older persisted blobs before any
  // consumer reads the layout, so tab switching and id-based edits are reliable.
  const layout = useMemo(() => dedupeLayoutIds(optimistic ?? fallback), [optimistic, fallback]);

  const setLayout = useCallback(
    (next: InboxLayout) => {
      setOptimistic(next);
      void (async () => {
        try {
          const updated = await api.updateMyPreferences({ [activeKey]: next });
          setUser(updated);
        } catch {
          // Roll back to the server truth on failure.
          setOptimistic(null);
        }
      })();
    },
    [activeKey, setUser],
  );

  const saveUserPreset = useCallback(
    (label: string) => {
      const cleanLabel = label.trim();
      if (!cleanLabel) return;
      void (async () => {
        try {
          const nextPresets = upsertUserInboxPreset(userPresets, {
            id: makeId("preset"),
            label: cleanLabel,
            layout: cloneInboxLayout(layout),
          });
          const updated = await api.updateMyPreferences({
            [USER_INBOX_PRESETS_KEY]: makeUserInboxPresetsBlob(nextPresets),
          });
          setUser(updated);
        } catch {
          // Keep the current menu state; the server remains the source of truth.
        }
      })();
    },
    [layout, setUser, userPresets],
  );

  const deleteUserPreset = useCallback(
    (presetId: string) => {
      void (async () => {
        try {
          const nextPresets = deleteUserInboxPreset(userPresets, presetId);
          const updated = await api.updateMyPreferences({
            [USER_INBOX_PRESETS_KEY]: makeUserInboxPresetsBlob(nextPresets),
          });
          setUser(updated);
        } catch {
          // Keep the current menu state; the server remains the source of truth.
        }
      })();
    },
    [setUser, userPresets],
  );

  return {
    layout,
    isMobileLayout,
    setLayout,
    userPresets,
    saveUserPreset,
    deleteUserPreset,
    hasSavedLayout: !!saved,
  };
}
