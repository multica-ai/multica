// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { parseWithFallback } from "../api/schema";
import {
  FALLBACK_KNOWLEDGE_ACCESS,
  FALLBACK_KNOWLEDGE_MODEL_CHECKS,
  FALLBACK_KNOWLEDGE_OWNER_LOGIN,
  FALLBACK_KNOWLEDGE_REPO_SYNC,
  FALLBACK_KNOWLEDGE_SOURCES,
  KnowledgeAccessSchema,
  KnowledgeBaseSettingsSchema,
  KnowledgeModelChecksSchema,
  KnowledgeOwnerLoginSchema,
  KnowledgeRepoSyncSchema,
  KnowledgeSourcesSchema,
} from "./platform-schemas";
import { knowledgeAccessOptions, knowledgePlatformKeys } from "./platform-queries";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    status === 204
      ? new Response(null, { status })
      : new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const source = {
  id: "ds1",
  knowledge_base: "company",
  name: "Engineering handbook",
  type: "notion",
  status: "error",
  sync_schedule: "0 */6 * * *",
  last_sync_at: "2026-11-02T03:00:00Z",
  error_message: "401 from Notion: integration token expired",
  total_items_synced: 42,
};

describe("knowledge repo sync schema", () => {
  it("parses the setting and the last run's outcome", () => {
    const parsed = parseWithFallback(
      {
        enabled: true,
        repo_url: "https://example.test/acme/app.git",
        branch: "main",
        docs_path: "docs",
        knowledge_base_key: "engineering",
        last_synced_at: "2026-11-02T03:00:00Z",
        last_error: "docs/a.md: lacks the required headings",
      },
      KnowledgeRepoSyncSchema,
      FALLBACK_KNOWLEDGE_REPO_SYNC,
      { endpoint: "GET /api/knowledge/repo-sync" },
    );
    expect(parsed).toMatchObject({ enabled: true, knowledge_base_key: "engineering", last_error: "docs/a.md: lacks the required headings" });
  });

  it("defaults an older server's setting to off, on main and docs, and never to syncing", () => {
    const parsed = KnowledgeRepoSyncSchema.parse({});
    expect(parsed).toMatchObject({ enabled: false, branch: "main", docs_path: "docs", knowledge_base_key: "engineering", last_synced_at: null, last_error: "" });
  });

  it("falls back to off when the setting is malformed", () => {
    const parsed = parseWithFallback(
      { enabled: "yes please", branch: 7 },
      KnowledgeRepoSyncSchema,
      FALLBACK_KNOWLEDGE_REPO_SYNC,
      { endpoint: "GET /api/knowledge/repo-sync" },
    );
    expect(parsed.enabled).toBe(false);
  });
});

describe("knowledge sources schema", () => {
  it("parses a failed source with its last sync and error", () => {
    const parsed = parseWithFallback(
      { state: "ready", ui_url: "http://ui.test", sources: [source] },
      KnowledgeSourcesSchema,
      FALLBACK_KNOWLEDGE_SOURCES,
      { endpoint: "GET /api/knowledge/sources" },
    );
    expect(parsed.sources[0]).toMatchObject({ status: "error", error_message: source.error_message, total_items_synced: 42 });
    expect(parsed.ui_url).toBe("http://ui.test");
  });

  it("treats a source that was never synced as having no sync time", () => {
    const parsed = KnowledgeSourcesSchema.parse({ state: "ready", sources: [{ id: "ds2", last_sync_at: undefined }] });
    expect(parsed.sources[0]!.last_sync_at).toBeNull();
    expect(parsed.sources[0]!.error_message).toBe("");
  });

  it("degrades an unknown state and a missing source list", () => {
    const parsed = KnowledgeSourcesSchema.parse({ state: "melting" });
    expect(parsed).toMatchObject({ state: "unknown", sources: [] });
  });

  it("falls back for a malformed body", () => {
    const parsed = parseWithFallback("not an object", KnowledgeSourcesSchema, FALLBACK_KNOWLEDGE_SOURCES, {
      endpoint: "GET /api/knowledge/sources",
    });
    expect(parsed).toEqual(FALLBACK_KNOWLEDGE_SOURCES);
  });
});

describe("knowledge access schema", () => {
  it("keeps known subjects and access levels and marks unknown ones", () => {
    const parsed = KnowledgeAccessSchema.parse({
      key: "product",
      visibility: "restricted",
      grants: [
        { subject_type: "squad", subject_id: "s1", access: "read" },
        { subject_type: "team", subject_id: "t1", access: "admin" },
      ],
    });
    expect(parsed.visibility).toBe("restricted");
    expect(parsed.grants).toEqual([
      { subject_type: "squad", subject_id: "s1", access: "read" },
      { subject_type: "unknown", subject_id: "t1", access: "unknown" },
    ]);
  });

  it("never reads an unknown visibility as restricted or workspace", () => {
    expect(KnowledgeAccessSchema.parse({ visibility: "public" }).visibility).toBe("unknown");
    expect(KnowledgeAccessSchema.parse({}).grants).toEqual([]);
  });

  it("falls back whole for a grant list that is not a list", () => {
    const parsed = parseWithFallback({ visibility: "restricted", grants: "all" }, KnowledgeAccessSchema, FALLBACK_KNOWLEDGE_ACCESS, {
      endpoint: "GET /api/knowledge/bases/:key/access",
    });
    expect(parsed).toEqual(FALLBACK_KNOWLEDGE_ACCESS);
  });
});

describe("knowledge settings, models and owner login schemas", () => {
  it("defaults the switches to off when the server omits them", () => {
    expect(KnowledgeBaseSettingsSchema.parse({ key: "company" })).toEqual({
      key: "company",
      wiki_enabled: false,
      summaries_enabled: false,
    });
  });

  it("keeps the reason a model failed", () => {
    const parsed = KnowledgeModelChecksSchema.parse({
      configured: true,
      checks: [{ role: "rerank", name: "rerank-model", ok: false, error: "upstream returned 404" }],
    });
    expect(parsed.checks[0]).toMatchObject({ ok: false, error: "upstream returned 404" });
  });

  it("reads a model check with a missing ok flag as failed, never as passed", () => {
    expect(KnowledgeModelChecksSchema.parse({ configured: true, checks: [{ role: "chat" }] }).checks[0]!.ok).toBe(false);
  });

  it("falls back to an unconfigured model set for a malformed body", () => {
    const parsed = parseWithFallback("oops", KnowledgeModelChecksSchema, FALLBACK_KNOWLEDGE_MODEL_CHECKS, {
      endpoint: "POST /api/knowledge/models/test",
    });
    expect(parsed).toEqual(FALLBACK_KNOWLEDGE_MODEL_CHECKS);
  });

  it("returns an empty login when the reveal response is unreadable", () => {
    const parsed = parseWithFallback(null, KnowledgeOwnerLoginSchema, FALLBACK_KNOWLEDGE_OWNER_LOGIN, {
      endpoint: "POST /api/knowledge/owner-account/reveal",
    });
    expect(parsed.password).toBe("");
  });
});

describe("knowledge platform api client", () => {
  const client = () => new ApiClient("http://api.test");

  it("lists sources through the adapter route and parses them", async () => {
    const fetchMock = respond({ state: "ready", ui_url: "", sources: [source] });
    const list = await client().listKnowledgeSources();
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/sources");
    expect(list.sources[0]).toMatchObject({ name: "Engineering handbook", status: "error" });
  });

  it("encodes the base key and the subject id in an access request", async () => {
    const fetchMock = respond({ key: "product", visibility: "restricted", grants: [] });
    await client().putKnowledgeGrant("product", "squad", "s/1", "write");
    const [url, init] = fetchMock.mock.calls[0]! as [string, RequestInit];
    expect(url).toBe("http://api.test/api/knowledge/bases/product/access/squad/s%2F1");
    expect(init.method).toBe("PUT");
    expect(JSON.parse(String(init.body))).toEqual({ access: "write" });
  });

  it("removes a grant with DELETE", async () => {
    const fetchMock = respond({ key: "product", visibility: "restricted", grants: [] });
    await client().deleteKnowledgeGrant("product", "agent", "a1");
    const [url, init] = fetchMock.mock.calls[0]! as [string, RequestInit];
    expect(url).toBe("http://api.test/api/knowledge/bases/product/access/agent/a1");
    expect(init.method).toBe("DELETE");
  });

  it("sends the visibility a base is set to", async () => {
    const fetchMock = respond({ key: "product", visibility: "restricted", grants: [] });
    await client().setKnowledgeVisibility("product", "restricted");
    const [url, init] = fetchMock.mock.calls[0]! as [string, RequestInit];
    expect(url).toBe("http://api.test/api/knowledge/bases/product/visibility");
    expect(JSON.parse(String(init.body))).toEqual({ visibility: "restricted" });
  });

  it("sends only the switches a patch names", async () => {
    const fetchMock = respond({ key: "company", wiki_enabled: true, summaries_enabled: true });
    await client().updateKnowledgeBaseSettings("company", { wiki_enabled: true });
    const [, init] = fetchMock.mock.calls[0]! as [string, RequestInit];
    expect(JSON.parse(String(init.body))).toEqual({ wiki_enabled: true });
  });

  it("surfaces a refused upload with the server's message", async () => {
    respond({ error: "a file cannot exceed 50 MB" }, 413);
    await expect(
      client().uploadKnowledgeDocument({ kb: "market", kind: "market_report", file: new File(["x"], "a.md") }),
    ).rejects.toThrow("a file cannot exceed 50 MB");
  });

  it("builds the access query key from the workspace and the base", () => {
    expect(knowledgeAccessOptions("ws1", "product").queryKey).toEqual(["knowledge", "ws1", "access", "product"]);
    expect(knowledgePlatformKeys.sources("ws1")).toEqual(["knowledge", "ws1", "sources"]);
  });

  it("does not fetch the access of a base with no key", () => {
    expect(knowledgeAccessOptions("ws1", "").enabled).toBe(false);
  });

  it("keeps the api error class for a refused reveal", async () => {
    respond({ error: "the knowledge password was already shown" }, 409);
    await expect(client().revealKnowledgeOwnerPassword()).rejects.toBeInstanceOf(ApiError);
  });
});
