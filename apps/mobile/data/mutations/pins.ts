/**
 * Pin mutations. Optimistic three-step (snapshot → patch → settle invalidate)
 * mirroring web `packages/core/pins/mutations.ts`. The toggle UX on a phone
 * is "tap pin → row should appear/disappear in sidebar in the next frame",
 * so optimistic is load-bearing — without it the user taps Pin, watches the
 * action sheet dismiss, and sees nothing happen until the WS event lands.
 *
 * Both mutations key on (wsId, userId). userId comes from the auth store
 * because the cache itself is per-user-per-workspace (see pinKeys).
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { PinnedItem, PinnedItemType } from "@multica/core/types";
import { api } from "@/data/api";
import { pinKeys } from "@/data/queries/pins";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCreatePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: (data: { item_type: PinnedItemType; item_id: string }) =>
      api.createPin(data),
    onMutate: async (data) => {
      if (!wsId || !userId) return;
      const key = pinKeys.list(wsId, userId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<PinnedItem[]>(key);
      // Optimistic stub. Real id arrives in onSuccess; until then a stable
      // synthetic id keeps any list keyExtractor happy. Position = max + 1
      // so it sorts last visually (matches server's APPEND semantics).
      const stub: PinnedItem = {
        id: `optimistic-${data.item_type}-${data.item_id}`,
        workspace_id: wsId,
        user_id: userId,
        item_type: data.item_type,
        item_id: data.item_id,
        position: (prev?.length ?? 0) + 1,
        created_at: new Date().toISOString(),
      };
      qc.setQueryData<PinnedItem[]>(key, (old) =>
        old ? [...old, stub] : [stub],
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.key && ctx.prev !== undefined) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (newPin) => {
      if (!wsId || !userId) return;
      const key = pinKeys.list(wsId, userId);
      // Swap the optimistic stub for the real pin so the id is server-issued.
      qc.setQueryData<PinnedItem[]>(key, (old) =>
        old
          ? old.map((p) =>
              p.id ===
              `optimistic-${newPin.item_type}-${newPin.item_id}`
                ? newPin
                : p,
            )
          : [newPin],
      );
    },
    onSettled: () => {
      if (!wsId || !userId) return;
      qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });
    },
  });
}

export function useDeletePin() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: ({
      itemType,
      itemId,
    }: {
      itemType: PinnedItemType;
      itemId: string;
    }) => api.deletePin(itemType, itemId),
    onMutate: async ({ itemType, itemId }) => {
      if (!wsId || !userId) return;
      const key = pinKeys.list(wsId, userId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<PinnedItem[]>(key);
      qc.setQueryData<PinnedItem[]>(key, (old) =>
        old
          ? old.filter(
              (p) => !(p.item_type === itemType && p.item_id === itemId),
            )
          : old,
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.key && ctx.prev !== undefined) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      if (!wsId || !userId) return;
      qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });
    },
  });
}
