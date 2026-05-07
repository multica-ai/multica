import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { ApiClient } from "./client";
import { parseWithFallback } from "./schema";

// Helper: stub fetch with a single JSON response. Status defaults to 200.
function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

// These tests cover the five failure modes that white-screened the desktop
// app in past incidents. The contract is: a malformed response degrades to
// an empty/safe shape, never throws into React.
describe("ApiClient schema fallback", () => {
  describe("listTimeline", () => {
    it("falls back to an empty page when required fields are missing", async () => {
      stubFetchJson({});
      const client = new ApiClient("https://api.example.test");
      const page = await client.listTimeline("issue-1");
      expect(page).toEqual({
        entries: [],
        next_cursor: null,
        prev_cursor: null,
        has_more_before: false,
        has_more_after: false,
      });
    });

    it("falls back when a field has the wrong type", async () => {
      stubFetchJson({
        entries: "not-an-array",
        next_cursor: null,
        prev_cursor: null,
        has_more_before: false,
        has_more_after: false,
      });
      const client = new ApiClient("https://api.example.test");
      const page = await client.listTimeline("issue-1");
      expect(page.entries).toEqual([]);
      expect(page.has_more_after).toBe(false);
    });

    it("accepts a new entry type rather than crashing on enum drift", async () => {
      stubFetchJson({
        entries: [
          {
            type: "future_kind", // not in TS union
            id: "e-1",
            actor_type: "member",
            actor_id: "u-1",
            created_at: "2026-01-01T00:00:00Z",
          },
        ],
        next_cursor: null,
        prev_cursor: null,
        has_more_before: false,
        has_more_after: false,
      });
      const client = new ApiClient("https://api.example.test");
      const page = await client.listTimeline("issue-1");
      expect(page.entries).toHaveLength(1);
      expect(page.entries[0]?.type).toBe("future_kind");
    });

    // Forward-compat: when the server adds a new field to an existing
    // shape, `.loose()` lets it pass through unchanged. Without `.loose()`
    // zod 4 strips it, which would silently break a future TS type that
    // adopts the field — see schemas.ts header comment.
    it("preserves unknown fields the schema didn't list", async () => {
      stubFetchJson({
        entries: [
          {
            type: "comment",
            id: "e-1",
            actor_type: "member",
            actor_id: "u-1",
            created_at: "2026-01-01T00:00:00Z",
            // New server-side field not present in TimelineEntrySchema:
            future_field: { nested: "value" },
          },
        ],
        next_cursor: null,
        prev_cursor: null,
        has_more_before: false,
        has_more_after: false,
        // New top-level field not present in TimelinePageSchema:
        page_metadata: { took_ms: 42 },
      });
      const client = new ApiClient("https://api.example.test");
      const page = await client.listTimeline("issue-1");
      const raw = page as unknown as Record<string, unknown>;
      const entry = page.entries[0] as unknown as Record<string, unknown>;
      expect(entry.future_field).toEqual({ nested: "value" });
      expect(raw.page_metadata).toEqual({ took_ms: 42 });
    });

    it("returns an empty page when the body is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const page = await client.listTimeline("issue-1");
      expect(page.entries).toEqual([]);
    });

    it("treats null arrays as empty arrays", async () => {
      stubFetchJson({
        entries: null,
        next_cursor: null,
        prev_cursor: null,
        has_more_before: false,
        has_more_after: false,
      });
      const client = new ApiClient("https://api.example.test");
      const page = await client.listTimeline("issue-1");
      expect(page.entries).toEqual([]);
    });
  });

  describe("listIssues", () => {
    it("falls back to an empty list when the response is malformed", async () => {
      // `issues` having the wrong type triggers the fallback. An object
      // with only unexpected keys would *succeed* parsing now (every
      // declared field has a default) and just pass the extras through
      // via `.loose()`, so we use a wrong-type payload here instead.
      stubFetchJson({ issues: "not-an-array", total: 0 });
      const client = new ApiClient("https://api.example.test");
      const res = await client.listIssues();
      expect(res).toEqual({ issues: [], total: 0 });
    });
  });

  describe("listComments", () => {
    it("returns [] when the response is not an array", async () => {
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const comments = await client.listComments("issue-1");
      expect(comments).toEqual([]);
    });
  });

  describe("listIssueSubscribers", () => {
    it("returns [] when the response is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const subs = await client.listIssueSubscribers("issue-1");
      expect(subs).toEqual([]);
    });
  });

  describe("listChildIssues", () => {
    it("returns { issues: [] } when the issues field is missing", async () => {
      stubFetchJson({});
      const client = new ApiClient("https://api.example.test");
      const res = await client.listChildIssues("issue-1");
      expect(res).toEqual({ issues: [] });
    });
  });
});

// Direct tests for the helper, decoupled from any specific endpoint —
// guards against an endpoint refactor masking a regression in the helper.
describe("parseWithFallback", () => {
  const opts = { endpoint: "TEST /unit" };

  it("returns parsed data on success", () => {
    const schema = z.object({ id: z.string() });
    const out = parseWithFallback({ id: "x" }, schema, { id: "fallback" }, opts);
    expect(out).toEqual({ id: "x" });
  });

  it("returns the fallback when validation fails", () => {
    const schema = z.object({ id: z.string() });
    const fallback = { id: "fallback" };
    const out = parseWithFallback({ id: 123 }, schema, fallback, opts);
    expect(out).toBe(fallback);
  });

  it("returns the fallback when data is null", () => {
    const schema = z.object({ id: z.string() });
    const fallback = { id: "fallback" };
    const out = parseWithFallback(null, schema, fallback, opts);
    expect(out).toBe(fallback);
  });
});
