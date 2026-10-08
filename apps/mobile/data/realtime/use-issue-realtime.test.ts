import { QueryClient } from "@tanstack/react-query";
import type {
  CommentDeletedPayload,
  CommentUpdatedPayload,
  Issue,
} from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { issueKeys } from "@/data/queries/issue-keys";
import { useIssueRealtime } from "./use-issue-realtime";

type EventHandler = (payload: unknown) => void;

interface MockWS {
  on: ReturnType<typeof vi.fn>;
  onReconnect: ReturnType<typeof vi.fn>;
}

type SubscriptionSetup = (ws: MockWS, wsId: string) => (() => void)[];

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  subscriptionSetups: [] as SubscriptionSetup[],
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => state.qc,
}));

vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: SubscriptionSetup) => {
    state.subscriptionSetups.push(setup);
  },
}));

const wsId = "workspace-1";
const issueId = "issue-1";
const detailKey = issueKeys.detail(wsId, issueId);

// Runs the setup useIssueRealtime registered and returns an emitter for the
// events it subscribed to.
function connect() {
  expect(state.subscriptionSetups).toHaveLength(1);
  const handlers = new Map<string, EventHandler>();
  const ws: MockWS = {
    on: vi.fn((event: string, handler: EventHandler) => {
      handlers.set(event, handler);
      return () => {};
    }),
    onReconnect: vi.fn(() => () => {}),
  };
  state.subscriptionSetups[0](ws, wsId);
  return (event: string, payload: unknown) => {
    const handler = handlers.get(event);
    if (!handler) throw new Error(`no handler for ${event}`);
    handler(payload);
  };
}

function deletedPayload(issueRevision?: number): CommentDeletedPayload {
  return { comment_id: "comment-1", issue_id: issueId, issue_revision: issueRevision };
}

// #8296: deleting a comment that has replies tombstones it via comment:updated.
function tombstonePayload(issueRevision?: number): CommentUpdatedPayload {
  return {
    comment: {
      id: "comment-1",
      issue_id: issueId,
      author_type: "member",
      author_id: "user-1",
      content: "",
      type: "comment",
      parent_id: null,
      reactions: [],
      attachments: [],
      created_at: "2026-09-11T07:00:00Z",
      updated_at: "2026-09-11T08:00:00Z",
      revision: 2,
      resolved_at: null,
      resolved_by_type: null,
      resolved_by_id: null,
      deleted_at: "2026-09-11T08:00:00Z",
    },
    issue_revision: issueRevision,
  };
}

describe("useIssueRealtime owner issue revision on comment deletes", () => {
  beforeEach(() => {
    state.qc = new QueryClient();
    state.subscriptionSetups.length = 0;
  });

  it.each([
    ["comment:deleted", deletedPayload],
    ["comment:updated", tombstonePayload],
  ] as const)("%s with issue_revision refetches only an older cached issue", (event, payload) => {
    state.qc.setQueryData<Issue>(detailKey, { id: issueId, revision: 5 } as Issue);
    useIssueRealtime(issueId);
    const emit = connect();

    emit(event, payload(5));
    expect(state.qc.getQueryState(detailKey)?.isInvalidated).toBe(false);

    emit(event, payload(6));
    expect(state.qc.getQueryState(detailKey)?.isInvalidated).toBe(true);
  });

  it.each([
    ["comment:deleted", deletedPayload],
    ["comment:updated", tombstonePayload],
  ] as const)("%s without issue_revision refetches every loaded owner projection", (event, payload) => {
    const issue = { id: issueId, revision: 7 } as Issue;
    const myKey = issueKeys.myList(wsId, "assigned", { assignee_id: "user-1" });
    state.qc.setQueryData<Issue>(detailKey, issue);
    state.qc.setQueryData<Issue[]>(myKey, [issue]);
    state.qc.setQueryData<Issue[]>(issueKeys.list(wsId), [issue]);
    useIssueRealtime(issueId);
    const emit = connect();

    emit(event, payload());

    for (const key of [detailKey, myKey, issueKeys.list(wsId)]) {
      expect(state.qc.getQueryState(key)?.isInvalidated).toBe(true);
    }
  });
});
