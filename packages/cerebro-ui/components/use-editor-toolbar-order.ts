"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  EDITOR_TOOLBAR_ORDER_KEY,
  readEditorToolbarRow,
  type EditorToolbarActionId,
  type EditorToolbarRow,
} from "./editor-toolbar-preferences";

export function useEditorToolbarOrder() {
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const saved = useMemo(
    () => readEditorToolbarRow(user?.preferences?.[EDITOR_TOOLBAR_ORDER_KEY]),
    [user?.preferences],
  );
  const [optimistic, setOptimistic] = useState<EditorToolbarRow | null>(null);

  useEffect(() => {
    setOptimistic(null);
  }, [saved]);

  const saveRow = useCallback(
    async (next: EditorToolbarRow) => {
      setOptimistic(next);
      try {
        const updated = await api.updateMyPreferences({
          [EDITOR_TOOLBAR_ORDER_KEY]: next,
        });
        setUser(updated);
      } catch (error) {
        setOptimistic(null);
        throw error;
      }
    },
    [setUser],
  );

  const row = optimistic ?? saved;

  const saveOrder = useCallback(
    (order: EditorToolbarActionId[]) => saveRow({ ...row, order }),
    [row, saveRow],
  );

  return {
    order: row.order,
    hidden: row.hidden,
    saveOrder,
    saveRow,
    canSave: Boolean(user),
  };
}
