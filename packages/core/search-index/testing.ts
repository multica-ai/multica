import type {
  SearchIndexChanges,
  SearchIndexChatMessage,
  SearchIndexChatSession,
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

export function chatSessionRecord(id: string, title = "", status = "active"): SearchIndexChatSession {
  return { id, agent_id: `agent-${id}`, title, status };
}

export function chatMessageRecord(
  id: string,
  sessionId: string,
  content: string,
  createdAt = "2026-01-01T00:00:00Z",
): SearchIndexChatMessage {
  return { id, chat_session_id: sessionId, role: "user", content, created_at: createdAt };
}

export type Change =
  | { kind: "issue"; record: SearchIndexIssue }
  | { kind: "comment"; record: SearchIndexComment }
  | { kind: "delete-issue"; id: string }
  | { kind: "delete-comment"; id: string }
  | { kind: "session"; record: SearchIndexChatSession }
  | { kind: "message"; record: SearchIndexChatMessage }
  | { kind: "delete-session"; id: string }
  | { kind: "delete-message"; id: string };

/** A server with a numbered change log; cursors are "c<version>". */
export class FakeServer implements IndexFetcher {
  issues = new Map<string, SearchIndexIssue>();
  comments = new Map<string, SearchIndexComment>();
  sessions = new Map<string, SearchIndexChatSession>();
  messages = new Map<string, SearchIndexChatMessage>();
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
    if (change.kind === "session") this.sessions.set(change.record.id, change.record);
    if (change.kind === "message") this.messages.set(change.record.id, change.record);
    if (change.kind === "delete-session") {
      this.sessions.delete(change.id);
      for (const [id, message] of this.messages) {
        if (message.chat_session_id === change.id) this.messages.delete(id);
      }
    }
    if (change.kind === "delete-message") this.messages.delete(change.id);
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

  async snapshot(afterNumber: number, afterMessageId: string, limit: number): Promise<SearchIndexSnapshotPage> {
    this.take("snapshot");
    const page = [...this.issues.values()]
      .filter((i) => i.number > afterNumber)
      .sort((a, b) => a.number - b.number)
      .slice(0, Math.min(limit, 2));
    const ids = new Set(page.map((i) => i.id));
    // Messages page by id, the way the server does; the fixture keeps every
    // id after the cursor in insertion order, which is enough for the tests.
    const allMessages = [...this.messages.values()].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
    const messages = allMessages.filter((m) => afterMessageId === "" || m.id > afterMessageId).slice(0, limit);
    return {
      issues: page,
      comments: [...this.comments.values()].filter((c) => ids.has(c.issue_id)),
      projects: [],
      sessions: afterNumber === 0 && afterMessageId === "" ? [...this.sessions.values()] : [],
      messages,
      next_after_number: page.at(-1)?.number ?? afterNumber,
      next_after_message_id: messages.at(-1)?.id ?? afterMessageId,
      done: page.length < 2 && messages.length < limit,
    };
  }

  async changes(cursor: string): Promise<SearchIndexChanges> {
    this.take("changes");
    const from = Number(cursor.slice(1));
    const out: SearchIndexChanges = {
      issues: [],
      comments: [],
      projects: [],
      sessions: [],
      messages: [],
      deleted: { issues: [], comments: [], projects: [], sessions: [], messages: [] },
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
      } else if (change.kind === "delete-comment") {
        out.deleted.comments.push(change.id);
      } else if (change.kind === "session") {
        const live = this.sessions.get(change.record.id);
        if (live) out.sessions.push(live);
        else out.deleted.sessions.push(change.record.id);
      } else if (change.kind === "message") {
        const live = this.messages.get(change.record.id);
        if (live) out.messages.push(live);
        else out.deleted.messages.push(change.record.id);
      } else if (change.kind === "delete-session") {
        out.deleted.sessions.push(change.id);
      } else {
        out.deleted.messages.push(change.id);
      }
    }
    return out;
  }
}
