import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { isDocRevisionConflict } from "./queries";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi
    .fn()
    .mockResolvedValue(
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const validDoc = {
  id: "d1",
  workspace_id: "ws",
  folder_id: null,
  title: "Runbook",
  revision: 4,
  version_count: 2,
  created_by_type: "member",
  created_by_id: "u1",
  updated_by_type: "agent",
  updated_by_id: "a1",
  created_at: "2026-10-29T10:00:00Z",
  updated_at: "2026-10-29T11:00:00Z",
  content: "# Runbook",
};

describe("knowledge base client", () => {
  it("parses a doc and tolerates unknown enum values", async () => {
    respond({
      ...validDoc,
      format: "pdf",
      updated_by_type: "robot",
      extra: true,
    });
    const doc = await new ApiClient("https://api.example.test").getDoc("d1");
    expect(doc).toMatchObject({
      id: "d1",
      revision: 4,
      updatedByType: "member",
      workspaceId: "ws",
    });
  });

  it("rejects a response missing content or a valid revision", async () => {
    const client = new ApiClient("https://api.example.test");
    respond({...validDoc, content: undefined});
    await expect(client.getDoc("d1")).rejects.toThrow(/Malformed response/);
    respond({...validDoc, revision: 0});
    await expect(client.getDoc("d1")).rejects.toThrow(/Malformed response/);
  });

  it("rejects a malformed doc instead of handing the editor an empty body", async () => {
    respond({ id: "d1", content: 42 });
    await expect(
      new ApiClient("https://api.example.test").getDoc("d1"),
    ).rejects.toThrow(/Malformed response/);
  });

  it("asks for the saved state with a collab ticket only when told to", async () => {
    const client = new ApiClient("https://api.example.test");
    const fetchMock = respond({ enabled: true, token: "t", document: "doc:d1", expires_at: "2026-11-21T00:00:00Z", state: "AAE=" });
    expect(await client.createDocCollabTicket("d1", { state: true })).toEqual({
      enabled: true,
      token: "t",
      document: "doc:d1",
      expiresAt: "2026-11-21T00:00:00Z",
      state: "AAE=",
    });
    expect(fetchMock.mock.calls[0]![1].body).toBe(JSON.stringify({ state: true }));

    const plain = respond({ enabled: true, token: "t", document: "doc:d1" });
    expect(await client.createDocCollabTicket("d1")).not.toHaveProperty("state");
    expect(plain.mock.calls[0]![1].body).toBeUndefined();
  });

  it("treats a malformed collab ticket as live editing off", async () => {
    const client = new ApiClient("https://api.example.test");
    respond({ enabled: "yes", state: 42 });
    expect(await client.createDocCollabTicket("d1", { state: true })).toEqual({ enabled: false });
    respond({ enabled: true, document: "doc:d1" });
    expect(await client.createDocCollabTicket("d1")).toEqual({ enabled: false });
  });

  it("falls back to an empty tree and versions on malformed responses", async () => {
    const client = new ApiClient("https://api.example.test");
    respond({ folders: "nope" });
    expect(await client.listDocTree()).toEqual({ folders: [], docs: [] });
    respond({ versions: [{ number: "x" }] });
    expect(await client.listDocVersions("d1")).toEqual([]);
  });

  it("reads which discussion an agent's version came from, and tolerates a bad link", async () => {
    const client = new ApiClient("https://api.example.test");
    const version = {
      doc_id: "d1", number: 2, title: "Runbook", author_type: "agent", author_id: "a1",
      created_at: "2026-11-23T00:00:00Z", updated_at: "2026-11-23T00:00:00Z",
    };
    respond({ versions: [
      { ...version, doc_thread_id: "c1", doc_thread_quote: "ship on Friday" },
      { ...version, number: 1, doc_thread_id: 7 },
    ] });
    const [fromDiscussion, malformed] = await client.listDocVersions("d1");
    expect(fromDiscussion).toMatchObject({ docThreadId: "c1", docThreadQuote: "ship on Friday" });
    expect(malformed).toMatchObject({ number: 1, docThreadId: null, docThreadQuote: null });
  });

  it("lists a doc's discussion runs and falls back to none on a malformed response", async () => {
    const client = new ApiClient("https://api.example.test");
    const fetchMock = respond({ tasks: [
      { id: "t1", agent_id: "a1", status: "running", doc_thread_id: "c1", error: null, can_cancel: true },
      { id: "t2", status: 3 },
      { id: "t3", agent_id: "a2", status: "failed", doc_thread_id: "c1", can_cancel: "yes" },
    ] });
    expect(await client.listDocTaskRuns("d1")).toMatchObject([
      { id: "t1", agentId: "a1", docThreadId: "c1", canCancel: true },
      { id: "t3", canCancel: false, error: null },
    ]);
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain("/api/docs/d1/task-runs");
    respond({ tasks: "nope" });
    expect(await client.listDocTaskRuns("d1")).toEqual([]);
  });

  it("sends the base revision and surfaces a stale write as a revision conflict", async () => {
    const fetchMock = respond(
      {
        error: "resource changed since it was loaded",
        code: "revision_conflict",
        expected_revision: 3,
        actual_revision: 4,
      },
      409,
    );
    const error = await new ApiClient("https://api.example.test")
      .updateDoc("d1", { baseRevision: 3, content: "x" })
      .catch((e: unknown) => e);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      base_revision: 3,
      content: "x",
    });
    expect(error).toBeInstanceOf(ApiError);
    expect(isDocRevisionConflict(error)).toBe(true);
    expect(
      isDocRevisionConflict(
        new ApiError("x", 409, "Conflict", { code: "folder_not_empty" }),
      ),
    ).toBe(false);
  });

  it("treats a stale save that could not be merged as a revision conflict", async () => {
    respond(
      { error: "the doc changed in the same place", code: "doc_merge_conflict", actual_revision: 5 },
      409,
    );
    const error = await new ApiClient("https://api.example.test")
      .updateDoc("d1", { baseRevision: 3, content: "x" })
      .catch((e: unknown) => e);
    expect(isDocRevisionConflict(error)).toBe(true);
  });

  it("searches docs and keeps the hits it can read", async () => {
    const { content: _content, ...summary } = validDoc;
    const fetchMock = respond({
      docs: [
        { ...summary, match_source: "content", matched_snippet: "...rotate the keys..." },
        { ...summary, id: "d2", match_source: "someday", matched_snippet: null },
        { id: "", title: 7 },
      ],
    });
    const result = await new ApiClient("https://api.example.test").searchDocs({ q: "rotate keys", limit: 5 });
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      "https://api.example.test/api/docs/search?q=rotate+keys&limit=5",
    );
    expect(result.docs.map((d) => [d.id, d.matchSource, d.matchedSnippet])).toEqual([
      ["d1", "content", "...rotate the keys..."],
      ["d2", "content", null],
    ]);
  });

  it("falls back to no hits when the search response is malformed", async () => {
    respond({ results: "nope" });
    expect(await new ApiClient("https://api.example.test").searchDocs({ q: "x" })).toEqual({ docs: [] });
  });
});
