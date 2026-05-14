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
    it("falls back to an empty array when the body is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      expect(entries).toEqual([]);
    });

    it("falls back when the body is not an array", async () => {
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      expect(entries).toEqual([]);
    });

    it("accepts a new entry type rather than crashing on enum drift", async () => {
      stubFetchJson([
        {
          type: "future_kind", // not in TS union
          id: "e-1",
          actor_type: "member",
          actor_id: "u-1",
          created_at: "2026-01-01T00:00:00Z",
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      expect(entries).toHaveLength(1);
      expect(entries[0]?.type).toBe("future_kind");
    });

    // Forward-compat: when the server adds a new field to an existing
    // shape, `.loose()` lets it pass through unchanged. Without `.loose()`
    // zod 4 strips it, which would silently break a future TS type that
    // adopts the field — see schemas.ts header comment.
    it("preserves unknown fields the schema didn't list", async () => {
      stubFetchJson([
        {
          type: "comment",
          id: "e-1",
          actor_type: "member",
          actor_id: "u-1",
          created_at: "2026-01-01T00:00:00Z",
          // New server-side field not present in TimelineEntrySchema:
          future_field: { nested: "value" },
        },
      ]);
      const client = new ApiClient("https://api.example.test");
      const entries = await client.listTimeline("issue-1");
      const entry = entries[0] as unknown as Record<string, unknown>;
      expect(entry.future_field).toEqual({ nested: "value" });
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

  // Agent template catalog is hit by the desktop create-agent picker.
  // Installed desktop builds outlive any given server, so the shape MUST
  // survive future field renames / wrapping without crashing. Each test
  // here mirrors a concrete future drift we want to absorb.
  describe("listAgentTemplates", () => {
    it("falls back to [] when the body is null", async () => {
      stubFetchJson(null);
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls).toEqual([]);
    });

    it("defaults skills to [] when the field is missing from a template", async () => {
      // Future server: drops `skills` because the picker no longer reads
      // them. Picker code calls `template.skills.length` — must not throw.
      stubFetchJson([{ slug: "x", name: "X" }]);
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls).toHaveLength(1);
      expect(tmpls[0]?.skills).toEqual([]);
    });

    it("accepts the bare-array shape (current contract)", async () => {
      stubFetchJson([
        { slug: "a", name: "A", description: "", skills: [] },
        { slug: "b", name: "B", description: "", skills: [] },
      ]);
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls.map((t) => t.slug)).toEqual(["a", "b"]);
    });

    it("accepts a future {templates: [...]} envelope without breaking", async () => {
      // Server migrates to a paginated envelope. We unwrap so the picker
      // keeps working on the older bare-array consumer.
      stubFetchJson({
        templates: [{ slug: "a", name: "A", description: "", skills: [] }],
        total: 1,
      });
      const client = new ApiClient("https://api.example.test");
      const tmpls = await client.listAgentTemplates();
      expect(tmpls).toHaveLength(1);
      expect(tmpls[0]?.slug).toBe("a");
    });
  });

  describe("getAgentTemplate", () => {
    it("falls back to a minimal record carrying the requested slug", async () => {
      // Slug is part of the URL the user clicked — the fallback round-
      // trips it so the page header still makes sense after a parse miss.
      stubFetchJson({ wrong: "shape" });
      const client = new ApiClient("https://api.example.test");
      const detail = await client.getAgentTemplate("code-reviewer");
      expect(detail.slug).toBe("code-reviewer");
      expect(detail.skills).toEqual([]);
      expect(detail.instructions).toBe("");
    });

    it("defaults instructions to '' when the field is missing", async () => {
      stubFetchJson({
        slug: "code-reviewer",
        name: "Code Reviewer",
        description: "",
        skills: [],
      });
      const client = new ApiClient("https://api.example.test");
      const detail = await client.getAgentTemplate("code-reviewer");
      expect(detail.instructions).toBe("");
    });
  });

  describe("createAgentFromTemplate", () => {
    it("falls back to an empty agent when the response is malformed", async () => {
      // The agent was created server-side even though the client can't
      // parse the response — UI code reads `agent.id === ""` and skips
      // the navigation step rather than landing on `/agents/`.
      stubFetchJson({ unexpected: "shape" });
      const client = new ApiClient("https://api.example.test");
      const resp = await client.createAgentFromTemplate({
        template_slug: "x",
        name: "X",
        runtime_id: "rt-1",
      });
      expect(resp.agent.id).toBe("");
      expect(resp.imported_skill_ids).toEqual([]);
      expect(resp.reused_skill_ids).toEqual([]);
    });

    it("defaults imported_skill_ids / reused_skill_ids to [] when missing", async () => {
      stubFetchJson({ agent: { id: "agent-1" } });
      const client = new ApiClient("https://api.example.test");
      const resp = await client.createAgentFromTemplate({
        template_slug: "x",
        name: "X",
        runtime_id: "rt-1",
      });
      expect(resp.agent.id).toBe("agent-1");
      expect(resp.imported_skill_ids).toEqual([]);
      expect(resp.reused_skill_ids).toEqual([]);
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
