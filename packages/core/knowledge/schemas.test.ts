import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_KNOWLEDGE_DETAIL,
  EMPTY_LIST_KNOWLEDGE_ANALYTICS_RESPONSE,
  EMPTY_LIST_KNOWLEDGE_RESPONSE,
  KnowledgeDetailSchema,
  KnowledgeDraftDispatchedSchema,
  CuratorDraftTaskSchema,
  ListKnowledgeAnalyticsResponseSchema,
  ListKnowledgeResponseSchema,
} from "./schemas";

describe("knowledge response schemas", () => {
  it("falls back when the list response is malformed", () => {
    const parsed = parseWithFallback(
      { items: null, total: "wrong" },
      ListKnowledgeResponseSchema,
      EMPTY_LIST_KNOWLEDGE_RESPONSE,
      { endpoint: "GET /api/knowledge" },
    );

    expect(parsed).toEqual(EMPTY_LIST_KNOWLEDGE_RESPONSE);
  });

  it("defaults missing item arrays to an empty list", () => {
    const parsed = parseWithFallback(
      { total: 0 },
      ListKnowledgeResponseSchema,
      EMPTY_LIST_KNOWLEDGE_RESPONSE,
      { endpoint: "GET /api/knowledge" },
    );

    expect(parsed.items).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it("falls back when the analytics response is malformed", () => {
    const parsed = parseWithFallback(
      { items: null, total: "wrong" },
      ListKnowledgeAnalyticsResponseSchema,
      EMPTY_LIST_KNOWLEDGE_ANALYTICS_RESPONSE,
      { endpoint: "GET /api/knowledge/analytics" },
    );

    expect(parsed).toEqual(EMPTY_LIST_KNOWLEDGE_ANALYTICS_RESPONSE);
  });

  it("defaults missing analytics arrays to an empty list", () => {
    const parsed = parseWithFallback(
      { total: 0 },
      ListKnowledgeAnalyticsResponseSchema,
      EMPTY_LIST_KNOWLEDGE_ANALYTICS_RESPONSE,
      { endpoint: "GET /api/knowledge/analytics" },
    );

    expect(parsed.items).toEqual([]);
    expect(parsed.total).toBe(0);
  });

  it("falls back when a publish detail response is malformed", () => {
    const parsed = parseWithFallback(
      { item: null, sources: null },
      KnowledgeDetailSchema,
      EMPTY_KNOWLEDGE_DETAIL,
      { endpoint: "POST /api/knowledge/:id/publish/wiki" },
    );

    expect(parsed).toEqual(EMPTY_KNOWLEDGE_DETAIL);
  });

  it("accepts dispatched response with task_id", () => {
    const parsed = parseWithFallback(
      { status: "queued", task_id: "task-1", message: "dispatched" },
      KnowledgeDraftDispatchedSchema,
      { status: "queued" as const, task_id: "", message: "" },
      { endpoint: "POST /api/knowledge/drafts/from-issue" },
    );
    expect(parsed.task_id).toBe("task-1");
    expect(parsed.status).toBe("queued");
  });

  it("CuratorDraftTaskSchema accepts running status from server", () => {
    const parsed = parseWithFallback(
      { id: "t1", status: "running", draft_kind: "issue" },
      CuratorDraftTaskSchema,
      { id: "t1", status: "queued" as const, draft_kind: "" },
      { endpoint: "GET /api/knowledge/curator-drafts/:id" },
    );
    expect(parsed.status).toBe("running");
  });

  it("CuratorDraftTaskSchema falls back for unknown status", () => {
    const parsed = parseWithFallback(
      { id: "t1", status: "processing", draft_kind: "issue" },
      CuratorDraftTaskSchema,
      { id: "t1", status: "queued" as const, draft_kind: "" },
      { endpoint: "GET /api/knowledge/curator-drafts/:id" },
    );
    expect(parsed.status).toBe("queued");
  });

  it("CuratorDraftTaskSchema accepts completed with result", () => {
    const parsed = CuratorDraftTaskSchema.safeParse({
      id: "t1", status: "completed", draft_kind: "issue", result: { item: { id: "k1" } },
    });
    expect(parsed.success).toBe(true);
    if (parsed.success) {
      const result = parsed.data.result as Record<string, unknown> | undefined;
      expect(result?.item).toEqual({ id: "k1" });
    }
  });
});
