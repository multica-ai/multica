import * as React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";

// Wave 3 / G3 (TECH-3556) — interim edit lock on a note. Mirrors the server
// LockResponse (server/internal/cerebro/note/lock.go). The server resolves
// `live` (heartbeat within timeout) and `held_by_me`, so the client trusts
// those rather than re-deriving the timeout rule.
export const NoteLockSchema = z.object({
  note_id: z.string().default(""),
  locked_by: z.string().nullable().default(null),
  locked_at: z.string().default(""),
  lock_heartbeat_at: z.string().default(""),
  live: z.boolean().default(false),
  held_by_me: z.boolean().default(false),
  timeout_sec: z.number().default(90),
});

export type NoteLock = z.infer<typeof NoteLockSchema>;

const FREE_LOCK: NoteLock = {
  note_id: "",
  locked_by: null,
  locked_at: "",
  lock_heartbeat_at: "",
  live: false,
  held_by_me: false,
  timeout_sec: 90,
};

export function safeParseNoteLock(raw: unknown): NoteLock {
  const r = NoteLockSchema.safeParse(raw);
  if (r.success) return r.data;
  console.warn("[cerebro-notes] note lock response failed validation", r.error);
  return FREE_LOCK;
}

export const noteLockKeys = {
  byNote: (wsId: string, noteId: string) =>
    ["note-lock", wsId, noteId] as const,
};

// useNoteEditLock manages the single-writer lock for an open note editor.
// While `active`, it acquires the lock and heartbeats at a third of the
// timeout. It exposes whether the caller may edit, who holds it otherwise, and
// a takeOver() that steals a live lock. Releases on unmount / when inactive.
//
// State machine (what `canEdit` means):
//   - lock free or already mine → canEdit = true, heartbeat keeps it.
//   - lock held LIVE by someone else → canEdit = false, `blockedBy` set, the
//     editor goes read-only and offers "Take over" (force acquire).
export function useNoteEditLock(noteId: string, active: boolean) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [lock, setLock] = React.useState<NoteLock | null>(null);
  const [acquiring, setAcquiring] = React.useState(false);

  const apply = React.useCallback((next: NoteLock) => setLock(next), []);

  const acquire = React.useCallback(
    async (force: boolean) => {
      try {
        const res = await api.acquireNoteLock(noteId, force);
        apply(safeParseNoteLock(res));
        return true;
      } catch (err) {
        // 409 = a live lock held by someone else; the body carries who.
        if (err instanceof ApiError && err.status === 409) {
          apply(safeParseNoteLock(err.body));
          return false;
        }
        throw err;
      }
    },
    [noteId, apply],
  );

  // Acquire on activate; release on deactivate / unmount.
  React.useEffect(() => {
    if (!active || !noteId) return;
    let cancelled = false;
    setAcquiring(true);
    acquire(false).finally(() => {
      if (!cancelled) setAcquiring(false);
    });
    return () => {
      cancelled = true;
      // Best-effort release so the next editor isn't blocked until timeout.
      void api.releaseNoteLock(noteId).catch(() => {});
      qc.invalidateQueries({ queryKey: noteLockKeys.byNote(wsId, noteId) });
    };
  }, [active, noteId, acquire, qc, wsId]);

  // Heartbeat at a third of the timeout while we hold (or are trying for) it.
  React.useEffect(() => {
    if (!active || !noteId) return;
    const everyMs = Math.max(10, (lock?.timeout_sec ?? 90) / 3) * 1000;
    const t = setInterval(() => {
      void acquire(false);
    }, everyMs);
    return () => clearInterval(t);
  }, [active, noteId, acquire, lock?.timeout_sec]);

  const takeOver = React.useCallback(async () => {
    setAcquiring(true);
    try {
      await acquire(true);
    } finally {
      setAcquiring(false);
    }
  }, [acquire]);

  const blockedByOther =
    !!lock && lock.live && !lock.held_by_me && !!lock.locked_by;
  // Optimistic: before the first response, assume we can edit (free note is the
  // common case); a 409 immediately flips it to read-only.
  const canEdit = !blockedByOther;

  return { lock, canEdit, blockedByOther, acquiring, takeOver };
}

// useNoteLockStatus is a passive read of a note's lock (no acquire/heartbeat) —
// for surfaces that only want to show "X is editing" without taking the lock.
export function useNoteLockStatus(noteId: string | null | undefined) {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: noteLockKeys.byNote(wsId, noteId ?? ""),
    queryFn: async () => safeParseNoteLock(await api.getNoteLock(noteId as string)),
    enabled: Boolean(wsId && noteId),
    refetchInterval: 30 * 1000,
  });
}

export function useReleaseNoteLock() {
  return useMutation({ mutationFn: (noteId: string) => api.releaseNoteLock(noteId) });
}
