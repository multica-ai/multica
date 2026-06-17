import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_KNOWLEDGE_DETAIL,
  EMPTY_LIST_KNOWLEDGE_ANALYTICS_RESPONSE,
  EMPTY_LIST_KNOWLEDGE_RESPONSE,
  KnowledgeDetailSchema,
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
});
