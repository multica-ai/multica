/**
 * Pin realtime — listing-level. WS payloads for `pin:*` events are typed as
 * `unknown` in packages/core/types/events.ts (no shape contract carried),
 * so we invalidate-on-event instead of patch. The list is small and the
 * events are user-action-rare (pin/unpin in another client), so the cost
 * is negligible — cellular-data concerns favoring patch don't apply here.
 *
 * Keyed on (wsId, userId) because the pin cache is per-user-per-workspace
 * (see pinKeys factory).
 */
import { useQueryClient } from "@tanstack/react-query";
import { pinKeys } from "@/data/queries/pins";
import { useAuthStore } from "@/data/auth-store";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";

export function usePinsRealtime() {
  const qc = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);

  useWSSubscriptions(
    (ws, wsId) => {
      if (!userId) return undefined;
      const invalidate = () =>
        qc.invalidateQueries({ queryKey: pinKeys.list(wsId, userId) });

      return [
        ws.on("pin:created", invalidate),
        ws.on("pin:deleted", invalidate),
        ws.on("pin:reordered", invalidate),
        ws.onReconnect(invalidate),
      ];
    },
    [qc, userId],
  );
}
