import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useAuthStore } from "../auth";
import { pinKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type { PinnedItem, PinnedItemType } from "../types";

export function useCreatePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  return useMutation({
    mutationFn: (data: { item_type: PinnedItemType; item_id: string }) =>
      api.createPin(data),
    onSuccess: (newPin) => {
      qc.setQueryData<PinnedItem[]>(pinKeys.list(wsId, userId), (old) =>
        old ? [...old, newPin] : [newPin],
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });
    },
  });
}

export function useDeletePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  return useMutation({
    mutationFn: ({ itemType, itemId }: { itemType: PinnedItemType; itemId: string }) =>
      api.deletePin(itemType, itemId),
    onMutate: async ({ itemType, itemId }) => {
      await qc.cancelQueries({ queryKey: pinKeys.list(wsId, userId) });
      const prev = qc.getQueryData<PinnedItem[]>(pinKeys.list(wsId, userId));
      qc.setQueryData<PinnedItem[]>(pinKeys.list(wsId, userId), (old) =>
        old ? old.filter((p) => !(p.item_type === itemType && p.item_id === itemId)) : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(pinKeys.list(wsId, userId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });
    },
  });
}

export function useReorderPins() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  return useMutation({
    mutationFn: (reorderedPins: PinnedItem[]) => {
      const items = reorderedPins.map((p, i) => ({ id: p.id, position: i + 1 }));
      return api.reorderPins({ items });
    },
    onMutate: async (reorderedPins) => {
      await qc.cancelQueries({ queryKey: pinKeys.list(wsId, userId) });
      const prev = qc.getQueryData<PinnedItem[]>(pinKeys.list(wsId, userId));
      qc.setQueryData<PinnedItem[]>(pinKeys.list(wsId, userId), reorderedPins);
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(pinKeys.list(wsId, userId), ctx.prev);
    },
  });
}
