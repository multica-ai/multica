import { useEffect, useState } from "react";
import { useQueryClient, type QueryKey } from "@tanstack/react-query";
import { localV2SnapshotSchema, type LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import type { SessionScope } from "./client";
import { readStoredSnapshot, writeStoredSnapshot } from "./files";

/** How long a conversation stays unchanged before it is written down. */
const WRITE_DELAY_MS = 1_000;

export function snapshotStoreKey(scope: SessionScope, id: string) { return JSON.stringify([scope.userId, scope.workspaceId, scope.deviceId, id]); }

export function parseStoredSnapshot(raw: string | null): LocalV2Snapshot | null {
  if (!raw) return null;
  try {
    const parsed = localV2SnapshotSchema.safeParse(JSON.parse(raw));
    return parsed.success ? parsed.data : null;
  } catch { return null; }
}

/**
 * Restores the conversation as last read into the query cache, then keeps the
 * stored copy current. The connection subscribes after the restored sequence,
 * and the daemon answers snapshot_required when that position is unusable.
 * Returns whether the screen may read the conversation yet.
 */
export function useStoredSnapshot(scope: SessionScope, id: string, key: QueryKey) {
  const qc = useQueryClient();
  const storeKey = snapshotStoreKey(scope, id);
  const [restored, setRestored] = useState(false);
  useEffect(() => {
    let current = true;
    setRestored(false);
    void readStoredSnapshot(storeKey).then((raw) => {
      if (!current) return;
      const snapshot = parseStoredSnapshot(raw);
      // A snapshot already in memory is at least as new.
      if (snapshot && !qc.getQueryData(key)) qc.setQueryData(key, snapshot);
      setRestored(true);
    });
    return () => { current = false; };
    // The key derives from storeKey's inputs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc, storeKey]);
  useEffect(() => {
    if (!restored) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const write = () => {
      timer = undefined;
      const snapshot = qc.getQueryData<LocalV2Snapshot>(key);
      // A running turn changes constantly; its end triggers the next write.
      if (snapshot && !snapshot.turns.some((turn) => turn.status === "running")) writeStoredSnapshot(storeKey, JSON.stringify(snapshot));
    };
    const stop = qc.getQueryCache().subscribe((event) => {
      if (event.type !== "updated" || JSON.stringify(event.query.queryKey) !== JSON.stringify(key)) return;
      if (timer) clearTimeout(timer);
      timer = setTimeout(write, WRITE_DELAY_MS);
    });
    return () => { stop(); if (timer) { clearTimeout(timer); write(); } };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc, storeKey, restored]);
  return restored;
}
