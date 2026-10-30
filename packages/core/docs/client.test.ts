import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { isDocRevisionConflict } from "./queries";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const validDoc = {
  id: "d1",
  workspace_id: "ws",
  folder_id: null,
  format: "markdown",
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
  project_ids: ["p1"],
  last_seen_at: null,
};

describe("knowledge base client", () => {
  it("parses a doc and tolerates unknown enum values", async () => {
    respond({ ...validDoc, format: "pdf", updated_by_type: "robot", extra: true });
    const doc = await new ApiClient("https://api.example.test").getDoc("d1");
    expect(doc).toMatchObject({ id: "d1", revision: 4, format: "markdown", updated_by_type: "member", unread: false });
  });

  it("rejects a malformed doc instead of handing the editor an empty body", async () => {
    respond({ id: "d1", content: 42 });
    await expect(new ApiClient("https://api.example.test").getDoc("d1")).rejects.toThrow(/Malformed response/);
  });

  it("falls back to an empty tree, list and activity on malformed responses", async () => {
    const client = new ApiClient("https://api.example.test");
    respond({ folders: "nope" });
    expect(await client.listDocTree()).toEqual({ folders: [], docs: [] });
    respond({ versions: [{ number: "x" }] });
    expect(await client.listDocVersions("d1")).toEqual([]);
    respond({ items: 7 });
    expect(await client.listDocActivity()).toEqual([]);
    respond({ docs: null });
    expect(await client.listProjectDocs("p1")).toEqual([]);
  });

  it("sends the base revision and surfaces a stale write as a revision conflict", async () => {
    const fetchMock = respond(
      { error: "resource changed since it was loaded", code: "revision_conflict", expected_revision: 3, actual_revision: 4 },
      409,
    );
    const error = await new ApiClient("https://api.example.test")
      .updateDoc("d1", { base_revision: 3, content: "x" })
      .catch((e: unknown) => e);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({ base_revision: 3, content: "x" });
    expect(error).toBeInstanceOf(ApiError);
    expect(isDocRevisionConflict(error)).toBe(true);
    expect(isDocRevisionConflict(new ApiError("x", 409, "Conflict", { code: "folder_not_empty" }))).toBe(false);
  });
});
