import type {
  SearchIndexChanges,
  SearchIndexComment,
  SearchIndexIssue,
  SearchIndexManifest,
  SearchIndexSnapshotPage,
} from "../types";
import type { IndexFetcher } from "./sync";

// Shared fixtures for the search index tests.

export function issueRecord(number: number, title: string, description: string | null = null): SearchIndexIssue {
  return {
    id: `issue-${number}`,
    workspace_id: "ws",
    number,
    identifier: `MUL-${number}`,
    title,
    description,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    search_updated_at: `2026-01-01T00:00:00.00000${number % 10}Z`,
  } as SearchIndexIssue;
}

export function commentRecord(id: string, issueId: string, content: string): SearchIndexComment {
  return { id, issue_id: issueId, content, created_at: "2026-01-01T00:00:00Z" };
}

export type Change =
  | { kind: "issue"; record: SearchIndexIssue }
  | { kind: "comment"; record: SearchIndexComment }
  | { kind: "delete-issue"; id: string }
  | { kind: "delete-comment"; id: string };

/** A server with a numbered change log; cursors are "c<version>". */
export class FakeServer implements IndexFetcher {
  issues = new Map<string, SearchIndexIssue>();
  comments = new Map<string, SearchIndexComment>();
  log: Change[] = [];
  textBytes = 100;
  calls: string[] = [];
  failNext: Partial<Record<"manifest" | "snapshot" | "changes", Error>> = {};

  get version(): number {
    return this.log.length;
  }

  apply(change: Change): void {
    this.log.push(change);
    if (change.kind === "issue") this.issues.set(change.record.id, change.record);
    if (change.kind === "comment") this.comments.set(change.record.id, change.record);
    if (change.kind === "delete-issue") {
      this.issues.delete(change.id);
      for (const [id, comment] of this.comments) {
        if (comment.issue_id === change.id) this.comments.delete(id);
      }
    }
    if (change.kind === "delete-comment") this.comments.delete(change.id);
  }

  private take(op: "manifest" | "snapshot" | "changes"): void {
    this.calls.push(op);
    const err = this.failNext[op];
    if (err) {
      delete this.failNext[op];
      throw err;
    }
  }

  async manifest(): Promise<SearchIndexManifest> {
    this.take("manifest");
    return {
      cursor: `c${this.version}`,
      issue_count: this.issues.size,
      comment_count: this.comments.size,
      project_count: 0,
      text_bytes: this.textBytes,
    };
  }

  async snapshot(afterNumber: number, limit: number): Promise<SearchIndexSnapshotPage> {
    this.take("snapshot");
    const page = [...this.issues.values()]
      .filter((i) => i.number > afterNumber)
      .sort((a, b) => a.number - b.number)
      .slice(0, Math.min(limit, 2));
    const ids = new Set(page.map((i) => i.id));
    return {
      issues: page,
      comments: [...this.comments.values()].filter((c) => ids.has(c.issue_id)),
      projects: [],
      next_after_number: page.at(-1)?.number ?? afterNumber,
      done: page.length < 2,
    };
  }

  async changes(cursor: string): Promise<SearchIndexChanges> {
    this.take("changes");
    const from = Number(cursor.slice(1));
    const out: SearchIndexChanges = {
      issues: [],
      comments: [],
      projects: [],
      deleted: { issues: [], comments: [], projects: [] },
      cursor: `c${this.version}`,
      has_more: false,
    };
    for (const change of this.log.slice(from)) {
      if (change.kind === "issue") {
        const live = this.issues.get(change.record.id);
        if (live) out.issues.push(live);
        else out.deleted.issues.push(change.record.id);
      } else if (change.kind === "comment") {
        const live = this.comments.get(change.record.id);
        if (live) out.comments.push(live);
        else out.deleted.comments.push(change.record.id);
      } else if (change.kind === "delete-issue") {
        out.deleted.issues.push(change.id);
      } else {
        out.deleted.comments.push(change.id);
      }
    }
    return out;
  }
}
