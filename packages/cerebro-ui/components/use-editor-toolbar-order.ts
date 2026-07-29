"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  EDITOR_TOOLBAR_ORDER_KEY,
  readEditorToolbarOrder,
  type EditorToolbarActionId,
} from "./editor-toolbar-preferences";

export function useEditorToolbarOrder() {
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const savedOrder = useMemo(
    () =>
      readEditorToolbarOrder(
        user?.preferences?.[EDITOR_TOOLBAR_ORDER_KEY],
      ),
    [user?.preferences],
  );
  const [optimisticOrder, setOptimisticOrder] =
    useState<EditorToolbarActionId[] | null>(null);

  useEffect(() => {
    setOptimisticOrder(null);
  }, [savedOrder]);

  const saveOrder = useCallback(
    async (nextOrder: EditorToolbarActionId[]) => {
      setOptimisticOrder(nextOrder);
      try {
        const updated = await api.updateMyPreferences({
          [EDITOR_TOOLBAR_ORDER_KEY]: nextOrder,
        });
        setUser(updated);
      } catch (error) {
        setOptimisticOrder(null);
        throw error;
      }
    },
    [setUser],
  );

  return {
    order: optimisticOrder ?? savedOrder,
    saveOrder,
    canSave: Boolean(user),
  };
}
