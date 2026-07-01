import type { TimelineItem } from "./build-timeline";

/** Merged item: consecutive thinking or text chunks collapsed into one row. */
export interface MergedItem {
  /** First seq in the merged range — used as the stable key. */
  seq: number;
  type: TimelineItem["type"];
  /** Concatenated content from all chunks. */
  content?: string;
  /** For non-merged items, the original fields are preserved. */
  tool?: string;
  input?: Record<string, unknown>;
  output?: string;
  /** How many original items were merged. 1 = not merged. */
  mergedCount: number;
  /** Last seq in the merged range — for the "N–M" display. */
  lastSeq?: number;
}

const MERGEABLE = new Set(["thinking", "text"]);

export function mergeStreamingChunks(items: TimelineItem[]): MergedItem[] {
  const out: MergedItem[] = [];
  for (const item of items) {
    if (!MERGEABLE.has(item.type)) {
      out.push({ ...item, mergedCount: 1 });
      continue;
    }
    const last = out[out.length - 1];
    if (last && last.type === item.type) {
      last.content = (last.content ?? "") + (item.content ?? "");
      last.mergedCount++;
      last.lastSeq = item.seq;
    } else {
      out.push({ ...item, mergedCount: 1, lastSeq: item.seq > item.seq ? item.seq : undefined });
    }
  }
  // Clean up: only set lastSeq when actually merged
  for (const m of out) {
    if (m.mergedCount <= 1) delete m.lastSeq;
  }
  return out;
}
