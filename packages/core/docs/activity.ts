import type { DocActivityItem, DocAuthorType, DocFormat } from "../types";

/**
 * "Recently updated" is file-level: one row per doc per day saying who
 * changed it and how often, plus what agents said they changed. The server
 * sends versions; this folds them into rows.
 */
export interface DocActivityRow {
  docId: string;
  title: string;
  format: DocFormat;
  folderId: string | null;
  /** Local calendar day, YYYY-MM-DD. */
  day: string;
  /** Number of versions written that day. */
  edits: number;
  authors: { type: DocAuthorType; id: string }[];
  /** Messages left with that day's versions, newest first. */
  messages: { authorType: DocAuthorType; authorId: string; text: string }[];
  created: boolean;
  unread: boolean;
  latestAt: string;
  /** Newest version number touched that day. */
  latestVersion: number;
  /** Oldest version number touched that day. */
  firstVersion: number;
}

export interface DocActivityDay {
  day: string;
  rows: DocActivityRow[];
}

export function localDayKey(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Groups activity (newest first) into days, each with one row per doc. */
export function groupDocActivity(items: readonly DocActivityItem[], unreadOnly = false): DocActivityDay[] {
  const days: DocActivityDay[] = [];
  const rows = new Map<string, DocActivityRow>();
  for (const item of items) {
    const day = localDayKey(item.updated_at);
    const key = `${day}:${item.doc_id}`;
    let row = rows.get(key);
    if (!row) {
      row = {
        docId: item.doc_id,
        title: item.doc_title,
        format: item.doc_format,
        folderId: item.doc_folder_id,
        day,
        edits: 0,
        authors: [],
        messages: [],
        created: false,
        unread: false,
        latestAt: item.updated_at,
        latestVersion: item.number,
        firstVersion: item.number,
      };
      rows.set(key, row);
      let bucket = days[days.length - 1];
      if (!bucket || bucket.day !== day) {
        bucket = { day, rows: [] };
        days.push(bucket);
      }
      bucket.rows.push(row);
    }
    row.edits++;
    row.firstVersion = Math.min(row.firstVersion, item.number);
    row.latestVersion = Math.max(row.latestVersion, item.number);
    if (item.action === "create") row.created = true;
    if (item.unread) row.unread = true;
    if (!row.authors.some((a) => a.type === item.author_type && a.id === item.author_id)) {
      row.authors.push({ type: item.author_type, id: item.author_id });
    }
    if (item.message.trim()) {
      row.messages.push({ authorType: item.author_type, authorId: item.author_id, text: item.message.trim() });
    }
  }
  if (!unreadOnly) return days;
  return days
    .map((d) => ({ day: d.day, rows: d.rows.filter((r) => r.unread) }))
    .filter((d) => d.rows.length > 0);
}
