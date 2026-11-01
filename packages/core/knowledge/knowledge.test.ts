// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { parseWithFallback } from "../api/schema";
import { knowledgeErrorState } from "./errors";
import { knowledgeKeys, knowledgeSearchOptions, knowledgeStatusOptions } from "./queries";
import {
  FALLBACK_KNOWLEDGE_BASES,
  FALLBACK_KNOWLEDGE_DOCUMENT,
  FALLBACK_KNOWLEDGE_DOCUMENT_LIST,
  FALLBACK_KNOWLEDGE_SEARCH,
  FALLBACK_KNOWLEDGE_STATUS,
  KnowledgeBasesSchema,
  KnowledgeDocumentListSchema,
  KnowledgeDocumentSchema,
  KnowledgeSearchResultSchema,
  KnowledgeStatusSchema,
} from "./schemas";
import type { KnowledgeBases, KnowledgeDocument, KnowledgeDocumentList, KnowledgeSearchResult } from "./types";

afterEach(() => {
  vi.unstubAllGlobals();
});

const doc = {
  id: "d1",
  document_id: "w1",
  knowledge_base: "market",
  title: "Acme profile",
  state: "draft",
  kind: "manual",
  url: "",
  source_type: "issue",
  source_id: "i1",
  created_by_type: "agent",
  created_by_id: "a1",
  created_at: "2026-11-01T00:00:00Z",
  published_at: null,
  error: "",
};

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    status === 204
      ? new Response(null, { status })
      : new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function parseDoc(raw: unknown) {
  return parseWithFallback<KnowledgeDocument>(raw, KnowledgeDocumentSchema, FALLBACK_KNOWLEDGE_DOCUMENT, { endpoint: "t" });
}

describe("knowledge schemas", () => {
  it("parses a full document", () => {
    expect(parseDoc(doc)).toMatchObject({ id: "d1", state: "draft", source_id: "i1", published_at: null });
  });

  it("degrades an unknown document state to unknown without failing the row", () => {
    expect(parseDoc({ ...doc, state: "archived" })).toMatchObject({ id: "d1", state: "unknown" });
  });

  it("treats a missing state as unknown, never as published", () => {
    const { state: _state, ...rest } = doc;
    expect(parseDoc(rest).state).toBe("unknown");
  });

  it("fills defaults an older server omits and keeps unknown fields", () => {
    expect(parseDoc({ id: "d1", state: "published", revision: 3 })).toMatchObject({
      id: "d1",
      title: "",
      source_id: null,
      created_by_id: null,
      published_at: null,
      error: "",
      revision: 3,
    });
  });

  it("falls back for a malformed document", () => {
    expect(parseDoc({ id: 7 })).toEqual(FALLBACK_KNOWLEDGE_DOCUMENT);
    expect(parseDoc("nope")).toEqual(FALLBACK_KNOWLEDGE_DOCUMENT);
  });

  it("parses the bases and degrades an unknown knowledge state", () => {
    const ok = parseWithFallback<KnowledgeBases>(
      { offered: true, state: "ready", knowledge_bases: [{ id: "k1", key: "company", name: "Company", visibility: "workspace", position: 0 }] },
      KnowledgeBasesSchema,
      FALLBACK_KNOWLEDGE_BASES,
      { endpoint: "t" },
    );
    expect(ok).toMatchObject({ offered: true, state: "ready" });
    expect(ok.knowledge_bases[0]).toMatchObject({ key: "company", visibility: "workspace" });

    const future = parseWithFallback<KnowledgeBases>(
      { offered: true, state: "migrating", knowledge_bases: [{ id: "k2", key: "x", visibility: "private" }] },
      KnowledgeBasesSchema,
      FALLBACK_KNOWLEDGE_BASES,
      { endpoint: "t" },
    );
    expect(future.state).toBe("unknown");
    expect(future.knowledge_bases[0]!.visibility).toBe("unknown");
  });

  it("reads a null list as empty and a missing state as unknown", () => {
    const list = parseWithFallback<KnowledgeDocumentList>(
      { documents: null },
      KnowledgeDocumentListSchema,
      FALLBACK_KNOWLEDGE_DOCUMENT_LIST,
      { endpoint: "t" },
    );
    expect(list).toMatchObject({ state: "unknown", documents: [] });
    const search = parseWithFallback<KnowledgeSearchResult>(
      { state: "offline", hits: null },
      KnowledgeSearchResultSchema,
      FALLBACK_KNOWLEDGE_SEARCH,
      { endpoint: "t" },
    );
    expect(search).toMatchObject({ state: "offline", hits: [] });
  });

  it("falls back for malformed lists and searches", () => {
    expect(
      parseWithFallback(["x"], KnowledgeDocumentListSchema, FALLBACK_KNOWLEDGE_DOCUMENT_LIST, { endpoint: "t" }),
    ).toEqual(FALLBACK_KNOWLEDGE_DOCUMENT_LIST);
    expect(
      parseWithFallback({ state: "ready", hits: [{ score: "high" }] }, KnowledgeSearchResultSchema, FALLBACK_KNOWLEDGE_SEARCH, {
        endpoint: "t",
      }),
    ).toEqual(FALLBACK_KNOWLEDGE_SEARCH);
    expect(
      parseWithFallback(null, KnowledgeBasesSchema, FALLBACK_KNOWLEDGE_BASES, { endpoint: "t" }),
    ).toEqual(FALLBACK_KNOWLEDGE_BASES);
  });
});

describe("knowledge status schema", () => {
  it("parses the owner view with the web UI link", () => {
    expect(
      parseWithFallback(
        {
          offered: true,
          enabled: true,
          state: "ready",
          knowledge_bases: [{ id: "b1", key: "company", name: "Company", visibility: "workspace", position: 0 }],
          ui_url: "https://kb.example.test",
        },
        KnowledgeStatusSchema,
        FALLBACK_KNOWLEDGE_STATUS,
        { endpoint: "t" },
      ),
    ).toMatchObject({ offered: true, state: "ready", ui_url: "https://kb.example.test" });
  });

  it("reads a not-offered deployment as unoffered with an unknown state left unknown", () => {
    expect(
      parseWithFallback(
        { offered: false, reason: "not_configured", state: "rocket" },
        KnowledgeStatusSchema,
        FALLBACK_KNOWLEDGE_STATUS,
        { endpoint: "t" },
      ),
    ).toMatchObject({ offered: false, reason: "not_configured", state: "unknown", knowledge_bases: [], ui_url: "" });
  });

  it("falls back to the unoffered status for a malformed response", () => {
    expect(
      parseWithFallback(["not", "a", "status"], KnowledgeStatusSchema, FALLBACK_KNOWLEDGE_STATUS, { endpoint: "t" }),
    ).toEqual(FALLBACK_KNOWLEDGE_STATUS);
    expect(FALLBACK_KNOWLEDGE_STATUS).toMatchObject({ offered: false, state: "unknown" });
  });
});

describe("knowledge api client", () => {
  const client = () => new ApiClient("http://api.test");

  it("lists documents with filters and parses them", async () => {
    const fetchMock = respond({ state: "ready", documents: [doc] });
    const list = await client().listKnowledgeDocuments({ kb: "market", state: "draft" });
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/documents?kb=market&state=draft");
    expect(list).toMatchObject({ state: "ready" });
    expect(list.documents[0]).toMatchObject({ id: "d1", state: "draft" });
  });

  it("lists without a query string when unfiltered", async () => {
    const fetchMock = respond({ state: "ready", documents: [] });
    await client().listKnowledgeDocuments();
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/documents");
  });

  it("searches with the query encoded and returns the offline state", async () => {
    const fetchMock = respond({ state: "offline", hits: [] });
    const result = await client().searchKnowledge({ q: "pricing & plans", kb: "market", limit: 5 });
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/search?q=pricing+%26+plans&kb=market&limit=5");
    expect(result).toEqual({ state: "offline", hits: [] });
  });

  it("gives an unreadable search the unknown state", async () => {
    respond({ hits: "none" });
    expect(await client().searchKnowledge({ q: "x" })).toEqual(FALLBACK_KNOWLEDGE_SEARCH);
  });

  it("creates a document with the request body", async () => {
    const fetchMock = respond(doc, 201);
    const created = await client().createKnowledgeDocument({ kb: "market", title: "Acme profile", content: "text", idempotency_key: "k" });
    const init = fetchMock.mock.calls[0]![1] as RequestInit;
    expect(init.method).toBe("POST");
    expect(JSON.parse(String(init.body))).toEqual({ kb: "market", title: "Acme profile", content: "text", idempotency_key: "k" });
    expect(created.id).toBe("d1");
  });

  it("publishes, discards and deletes by record id", async () => {
    let fetchMock = respond({ ...doc, state: "published" });
    expect((await client().publishKnowledgeDocument("d1")).state).toBe("published");
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/documents/d1/publish");

    fetchMock = respond(null, 204);
    await client().discardKnowledgeDocument("d1");
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/documents/d1/discard");
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("POST");

    fetchMock = respond(null, 204);
    await client().deleteKnowledgeDocument("d1");
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("DELETE");
  });

  it("reads the status and enables knowledge with a POST", async () => {
    let fetchMock = respond({ offered: true, enabled: false, state: "disabled", ui_url: "" });
    const status = await client().getKnowledgeStatus();
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge");
    expect(status).toMatchObject({ offered: true, state: "disabled" });

    fetchMock = respond({ offered: true, enabled: true, state: "ready", ui_url: "https://kb.example.test" });
    const enabled = await client().enableKnowledge();
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/enable");
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("POST");
    expect(enabled).toMatchObject({ state: "ready", ui_url: "https://kb.example.test" });
  });

  it("surfaces the offline state of a failed write", async () => {
    respond({ error: "the knowledge service is offline", state: "offline" }, 503);
    const error = await client()
      .createKnowledgeDocument({ kb: "market", title: "t", content: "c" })
      .catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(knowledgeErrorState(error)).toBe("offline");
  });
});

describe("knowledgeErrorState", () => {
  const err = (status: number, body?: unknown) => new ApiError("x", status, "", body);
  it("reads the state of a 503 or 409", () => {
    expect(knowledgeErrorState(err(503, { state: "offline" }))).toBe("offline");
    expect(knowledgeErrorState(err(409, { state: "disabled" }))).toBe("disabled");
    expect(knowledgeErrorState(err(409, { state: "provisioning" }))).toBe("provisioning");
  });
  it("ignores everything else", () => {
    expect(knowledgeErrorState(err(409, { error: "already published" }))).toBeNull();
    expect(knowledgeErrorState(err(503, { state: "who knows" }))).toBeNull();
    expect(knowledgeErrorState(err(500, { state: "offline" }))).toBeNull();
    expect(knowledgeErrorState(new Error("boom"))).toBeNull();
    expect(knowledgeErrorState(undefined)).toBeNull();
  });
});

describe("knowledge query keys", () => {
  it("scopes every key to the workspace", () => {
    expect(knowledgeKeys.bases("ws1")).toEqual(["knowledge", "ws1", "bases"]);
    expect(knowledgeKeys.bases("ws1")).not.toEqual(knowledgeKeys.bases("ws2"));
    for (const key of [
      knowledgeKeys.documents("ws1"),
      knowledgeKeys.documentList("ws1", { kb: "market" }),
      knowledgeKeys.search("ws1", { q: "x" }),
    ]) {
      expect(key.slice(0, 2)).toEqual(["knowledge", "ws1"]);
    }
  });

  it("shares the documents prefix so one invalidation refreshes every filter", () => {
    const prefix = knowledgeKeys.documents("ws1");
    expect(knowledgeKeys.documentList("ws1", { kb: "market", state: "draft" }).slice(0, prefix.length)).toEqual(prefix);
    expect(knowledgeKeys.documentList("ws1").slice(0, prefix.length)).toEqual(prefix);
  });

  it("keys searches by query, base and limit", () => {
    expect(knowledgeKeys.search("ws1", { q: "a" })).not.toEqual(knowledgeKeys.search("ws1", { q: "b" }));
    expect(knowledgeKeys.search("ws1", { q: "a", kb: "x" })).not.toEqual(knowledgeKeys.search("ws1", { q: "a" }));
  });

  it("scopes the status key to the workspace", () => {
    expect(knowledgeKeys.status("ws1")).toEqual(["knowledge", "ws1", "status"]);
    expect(knowledgeStatusOptions("ws1").queryKey).toEqual(knowledgeKeys.status("ws1"));
  });

  it("does not search for a blank query", () => {
    expect(knowledgeSearchOptions("ws1", { q: "   " }).enabled).toBe(false);
    expect(knowledgeSearchOptions("ws1", { q: " pricing " })).toMatchObject({ enabled: true });
    expect(knowledgeSearchOptions("ws1", { q: " pricing " }).queryKey).toEqual(knowledgeKeys.search("ws1", { q: "pricing" }));
  });
});
