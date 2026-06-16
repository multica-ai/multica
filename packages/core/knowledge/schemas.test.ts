import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  EMPTY_LIST_KNOWLEDGE_RESPONSE,
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
});
