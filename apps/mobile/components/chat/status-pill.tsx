/**
 * In-flight task status pill — a compact version of web's
 * packages/views/chat/components/task-status-pill.tsx.
 *
 * v1 reads only `ChatPendingTask.status` + `created_at` (no taskMessages
 * introspection). Cycle:
 *
 *   status="queued"  → "Queued · Ns"
 *   status="running" → "Thinking · Ns"
 *
 * Hidden when `pendingTask` is null / empty. The owning chat screen
 * decides whether to mount this — there's no internal hide rule beyond
 * "show while there's a task id".
 *
 * Elapsed seconds tick locally via `setInterval(1000)` anchored to
 * `pendingTask.created_at` (server-authoritative). 1Hz is enough fidelity
 * — sub-second jitter from JS timer drift is invisible at this granularity.
 *
 * Stop button is the only action — the chat screen wires it to
 * api.cancelTaskById.
 */
import { useEffect, useState } from "react";
import { Pressable, View } from "react-native";
import type { ChatPendingTask } from "@multica/core/types";
import { Text } from "@/components/ui/text";

interface Props {
  pendingTask: ChatPendingTask | null | undefined;
  onStop: () => void;
}

export function StatusPill({ pendingTask, onStop }: Props) {
  const taskId = pendingTask?.task_id;
  const createdAt = pendingTask?.created_at;
  const status = pendingTask?.status;

  // Anchor for the elapsed-seconds counter. Falls back to `Date.now()` when
  // the server hasn't sent `created_at` yet (the brief seed window before
  // the POST returns).
  const anchorMs = createdAtToMs(createdAt) ?? Date.now();

  // `tickKey` exists only to force a re-render every second; the value
  // itself isn't read. setInterval is cheap, and we don't even mount the
  // pill when there's no in-flight task.
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!taskId) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [taskId]);

  if (!taskId) return null;

  const elapsedSec = Math.max(0, Math.floor((Date.now() - anchorMs) / 1000));
  const label = stageLabel(status, elapsedSec);

  return (
    <View className="mx-3 mb-2 flex-row items-center gap-2 rounded-full border border-border bg-secondary px-3 py-1.5">
      <View className="h-1.5 w-1.5 rounded-full bg-primary" />
      <Text className="flex-1 text-xs text-foreground">{label}</Text>
      <Pressable
        onPress={onStop}
        hitSlop={8}
        accessibilityLabel="Stop task"
        className="h-6 w-6 items-center justify-center rounded-md active:opacity-70"
      >
        <View className="h-3 w-3 rounded-sm bg-foreground" />
      </Pressable>
    </View>
  );
}

function stageLabel(status: string | undefined, elapsedSec: number): string {
  // Default to "Thinking" — the most common in-flight state. Unknown
  // status values from a newer server fall through here rather than
  // showing a raw enum string.
  if (status === "queued") return `Queued · ${elapsedSec}s`;
  return `Thinking · ${elapsedSec}s`;
}

function createdAtToMs(iso: string | undefined): number | null {
  if (!iso) return null;
  const ms = Date.parse(iso);
  return Number.isFinite(ms) ? ms : null;
}
