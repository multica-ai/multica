"use client";

import { useCallback, useMemo } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  EDITOR_TOOLBAR_LIST_TYPE_KEY,
  readEditorListType,
  type EditorListType,
} from "./editor-toolbar-preferences";

/**
 * The last list type the user reached for, persisted on the user record so the
 * lists split control opens on the same type on every device and after a
 * reload. Mirrors useEditorToolbarOrder: read from the auth store, write
 * through PATCH /api/me/preferences.
 */
export function useEditorListType() {
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const listType = useMemo(
    () => readEditorListType(user?.preferences?.[EDITOR_TOOLBAR_LIST_TYPE_KEY]),
    [user?.preferences],
  );

  const saveListType = useCallback(
    async (next: EditorListType) => {
      const updated = await api.updateMyPreferences({
        [EDITOR_TOOLBAR_LIST_TYPE_KEY]: next,
      });
      setUser(updated);
    },
    [setUser],
  );

  return { listType, saveListType };
}
