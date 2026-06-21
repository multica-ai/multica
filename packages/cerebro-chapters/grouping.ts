import type { TimelineEntry } from "@multica/core/types";
import type { Chapter } from "./types";

export interface ChapterGroup {
  chapter: Chapter;
  entries: TimelineEntry[];
  groups: { type: "activities" | "comment"; entries: TimelineEntry[] }[];
}

export function defaultChapter(issueId: string): Chapter {
  return {
    id: "default",
    issue_id: issueId,
    name: "Session 1",
    status: "in_progress",
    position: 0,
    handoff_summary: "",
    handoff_done: [],
    handoff_remaining: [],
    plan_ref: null,
    created_at: "",
    updated_at: "",
  };
}

export function groupTimelineByChapter(
  issueId: string,
  chapters: Chapter[] | undefined,
  groups: { type: "activities" | "comment"; entries: TimelineEntry[] }[],
): ChapterGroup[] {
  const fallback = defaultChapter(issueId);
  const byId = new Map((chapters ?? []).map((chapter) => [chapter.id, chapter]));
  const ordered = [...(chapters ?? [])].sort((a, b) => a.position - b.position);
  const buckets = new Map<string, TimelineEntry[]>();
  const groupBuckets = new Map<string, { type: "activities" | "comment"; entries: TimelineEntry[] }[]>();

  for (const group of groups) {
    const first = group.entries[0];
    if (!first) continue;
    const chapterId = group.type === "comment" ? (first.chapter_id ?? "default") : "default";
    const bucket = buckets.get(chapterId) ?? [];
    bucket.push(...group.entries);
    buckets.set(chapterId, bucket);
    const groupBucket = groupBuckets.get(chapterId) ?? [];
    groupBucket.push(group);
    groupBuckets.set(chapterId, groupBucket);
  }

  const out: ChapterGroup[] = [];
  for (const chapter of ordered) {
    out.push({ chapter, entries: buckets.get(chapter.id) ?? [], groups: groupBuckets.get(chapter.id) ?? [] });
    buckets.delete(chapter.id);
    groupBuckets.delete(chapter.id);
  }
  out.push({ chapter: fallback, entries: buckets.get("default") ?? [], groups: groupBuckets.get("default") ?? [] });
  buckets.delete("default");
  groupBuckets.delete("default");
  for (const [id, entries] of buckets) {
    out.push({ chapter: byId.get(id) ?? { ...fallback, id, name: "Session" }, entries, groups: groupBuckets.get(id) ?? [] });
  }
  return out.filter((group) => group.entries.length > 0 || group.chapter.id !== "default");
}
