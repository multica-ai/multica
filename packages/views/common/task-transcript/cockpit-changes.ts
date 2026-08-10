import type { TimelineItem } from "./build-timeline";
import {
  traceEventDetail,
  type TraceDiffLine,
  type TracePatchFile,
} from "./trace-event-presenter";

export type CockpitFileChangeKind = "add" | "delete" | "update";
export type CockpitFileChangeStatus = "applied" | "failed" | "pending";

export interface CockpitFileChange {
  path: string;
  changeKind: CockpitFileChangeKind;
  additions: number;
  deletions: number;
  /** False when the event reported a whole-file write without a before side. */
  hasLineCounts: boolean;
  status: CockpitFileChangeStatus;
  lastSeq: number;
}

interface ChangeDelta {
  path: string;
  changeKind: CockpitFileChangeKind;
  additions: number;
  deletions: number;
  hasLineCounts: boolean;
}

function normalizeChangeKind(kind: string | undefined): CockpitFileChangeKind {
  switch (kind) {
    case "add":
      return "add";
    case "delete":
      return "delete";
    default:
      return "update";
  }
}

function countDiffLines(lines: readonly TraceDiffLine[]): Pick<ChangeDelta, "additions" | "deletions"> {
  let additions = 0;
  let deletions = 0;
  for (const line of lines) {
    if (line.kind === "add") additions++;
    if (line.kind === "remove") deletions++;
  }
  return { additions, deletions };
}

function patchDelta(file: TracePatchFile): ChangeDelta {
  const path = file.movePath ? `${file.path} → ${file.movePath}` : file.path;
  const changeKind = normalizeChangeKind(file.changeKind);
  if (file.body.kind === "diff") {
    return {
      path,
      changeKind,
      ...countDiffLines(file.body.lines),
      hasLineCounts: true,
    };
  }
  if (file.body.kind === "file" && changeKind === "add") {
    return {
      path,
      changeKind,
      additions: file.body.lineCount,
      deletions: 0,
      hasLineCounts: true,
    };
  }
  return {
    path,
    changeKind,
    additions: 0,
    deletions: 0,
    hasLineCounts: false,
  };
}

function eventDeltas(item: TimelineItem): ChangeDelta[] {
  if (item.type !== "tool_use") return [];
  const detail = traceEventDetail(item);
  switch (detail.kind) {
    case "diff":
      return [{
        path: detail.path,
        changeKind: "update",
        ...countDiffLines(detail.lines),
        hasLineCounts: true,
      }];
    case "file":
      return [{
        path: detail.path,
        changeKind: "update",
        additions: 0,
        deletions: 0,
        hasLineCounts: false,
      }];
    case "patch":
      return detail.files.map(patchDelta);
    default:
      return [];
  }
}

interface PendingEdit {
  callID?: string;
  tool: string;
  deltas: ChangeDelta[];
  seq: number;
}

function normalizedTool(tool: string | undefined): string {
  return (tool ?? "").trim().toLowerCase();
}

function structuredResultFailed(value: unknown): boolean {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const record = value as Record<string, unknown>;
  if (record.success === false || record.is_error === true || record.isError === true) return true;
  const status = typeof record.status === "string" ? record.status.toLowerCase() : "";
  return status === "failed" || status === "error" || status === "declined" ||
    status === "cancelled" || status === "canceled";
}

function toolResultFailed(item: TimelineItem): boolean {
  const output = (item.output ?? item.content ?? "").trim();
  if (!output) return false;
  if (/^(failed|failure|error|declined|cancelled|canceled)\b/i.test(output)) return true;
  try {
    return structuredResultFailed(JSON.parse(output));
  } catch {
    return false;
  }
}

/**
 * Summarize file-mutation events already present in the live transcript.
 * Counts are cumulative edit activity for this run, not a replacement for a
 * fresh `git diff --numstat` snapshot of the worktree.
 */
export function summarizeCockpitFileChanges(items: readonly TimelineItem[]): CockpitFileChange[] {
  const byPath = new Map<string, CockpitFileChange>();
  const pending: PendingEdit[] = [];

  const record = (
    delta: ChangeDelta,
    status: CockpitFileChangeStatus,
    lastSeq: number,
  ) => {
    const previous = byPath.get(delta.path);
    const applied = status === "applied";
    byPath.set(delta.path, {
      path: delta.path,
      changeKind: delta.changeKind === "update"
        ? previous?.changeKind ?? "update"
        : delta.changeKind,
      additions: (previous?.additions ?? 0) + (applied ? delta.additions : 0),
      deletions: (previous?.deletions ?? 0) + (applied ? delta.deletions : 0),
      hasLineCounts: (previous?.hasLineCounts ?? false) || (applied && delta.hasLineCounts),
      status,
      lastSeq,
    });
  };

  for (const item of items) {
    const deltas = eventDeltas(item);
    if (deltas.length > 0) {
      pending.push({
        callID: item.call_id,
        tool: normalizedTool(item.tool),
        deltas,
        seq: item.seq,
      });
      continue;
    }
    if (item.type !== "tool_result") continue;

    const callID = item.call_id;
    const tool = normalizedTool(item.tool);
    let pendingIndex = callID
      ? pending.findIndex((candidate) => candidate.callID === callID)
      : pending.findIndex((candidate) => !candidate.callID && candidate.tool === tool);
    // During a rolling upgrade a result may already carry a provider call ID
    // while its persisted tool-use row came from an older daemon. Preserve the
    // legacy same-tool fallback only for an untagged candidate; never let a
    // mismatched non-empty call ID consume another concurrent edit.
    if (pendingIndex < 0 && callID) {
      pendingIndex = pending.findIndex(
        (candidate) => !candidate.callID && candidate.tool === tool,
      );
    }
    if (pendingIndex < 0) continue;
    const [edit] = pending.splice(pendingIndex, 1);
    if (!edit) continue;
    const status: CockpitFileChangeStatus = toolResultFailed(item) ? "failed" : "applied";
    for (const delta of edit.deltas) {
      record(delta, status, item.seq);
    }
  }

  for (const edit of pending) {
    for (const delta of edit.deltas) {
      record(delta, "pending", edit.seq);
    }
  }

  return Array.from(byPath.values()).sort(
    (a, b) => b.lastSeq - a.lastSeq || a.path.localeCompare(b.path),
  );
}
