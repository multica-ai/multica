import type { SearchIssuesResponse, SearchProjectsResponse } from "../types";
import type { IssueSearchParams } from "./engine";

/** Messages between a tab and the search index worker (MUL-7754). */

export interface IndexTarget {
  userId: string;
  workspaceId: string;
  /** Sent with every request the tab makes on the worker's behalf. */
  workspaceSlug: string;
}

export function indexTargetKey(target: Pick<IndexTarget, "userId" | "workspaceId">): string {
  return `${target.userId}:${target.workspaceId}`;
}

export type FetchOp = "manifest" | "snapshot" | "changes";

export type TabMessage =
  | { type: "attach"; target: IndexTarget }
  | { type: "detach" }
  /** The tab is going away (pagehide); forget its port. */
  | { type: "close" }
  | { type: "poke" }
  | { type: "ping" }
  | { type: "search"; id: number; kind: "issues" | "projects"; params: IssueSearchParams }
  | { type: "fetch-result"; id: number; ok: true; data: unknown }
  | { type: "fetch-result"; id: number; ok: false; status?: number; message: string }
  | { type: "wipe"; id: number }
  /** Access to the workspace ended (deleted, removed, left): destroy its copies. */
  | { type: "forget"; workspaceId: string }
  /** The user's current workspaces: destroy copies of any other workspace or user. */
  | { type: "prune"; userId: string; workspaceIds: string[] };

export type WorkerMessage =
  | { type: "hello" }
  | { type: "serving"; key: string; serving: boolean }
  | {
      type: "fetch";
      id: number;
      op: FetchOp;
      workspaceSlug: string;
      params: { afterNumber?: number; cursor?: string; limit?: number };
    }
  | { type: "search-result"; id: number; result: SearchIssuesResponse | SearchProjectsResponse | null }
  | { type: "wiped"; id: number };
