// @vitest-environment node

import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubJSON(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })));
}

describe("marketplace template API", () => {
  it("builds catalog filters and defaults additive list fields", async () => {
    stubJSON({
      templates: [{
        id: "tpl-1",
        source_workspace_id: "ws-1",
        created_by: "user-1",
        creator_name: "Ava",
        source_type: "squad",
        source_id: "squad-1",
        name: "Delivery",
        description: "A reusable delivery team",
        visibility: "public",
      }],
      total: 1,
      page: 2,
      page_size: 12,
    });
    const client = new ApiClient("https://api.example.test");

    const result = await client.listMarketplaceTemplates({
      query: "delivery",
      source_type: "squad",
      scope: "public",
      sort: "recent",
      page: 2,
      page_size: 12,
    });

    expect(result.templates[0]).toMatchObject({
      id: "tpl-1",
      tags: [],
      applied_count: 0,
      agent_count: 0,
      skill_count: 0,
      preview_agents: [],
      can_manage: false,
    });
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/templates?q=delivery&type=squad&scope=public&sort=recent&page=2&page_size=12",
    );
  });

  it("keeps an unreadable success body from becoming a partial template", async () => {
    stubJSON({ templates: [{ source_type: "future-kind" }], total: 1 });
    const client = new ApiClient("https://api.example.test");

    await expect(client.listMarketplaceTemplates()).resolves.toEqual({
      templates: [],
      total: 0,
      page: 1,
      page_size: 12,
    });
  });

  it("parses the safe template snapshot used by the import dialog", async () => {
    stubJSON({
      id: "tpl-1",
      source_workspace_id: "ws-1",
      created_by: "user-1",
      creator_name: "Ava",
      source_type: "agent",
      source_id: "agent-1",
      name: "Reviewer",
      description: "Review changes",
      visibility: "workspace",
      snapshot: {
        version: 1,
        source_type: "agent",
        agents: [{ key: "agent_1", name: "Reviewer", instructions: "Review", skill_keys: [] }],
        skills: [],
      },
    });
    const client = new ApiClient("https://api.example.test");

    const result = await client.getMarketplaceTemplate("tpl-1");

    expect(result.snapshot?.agents[0]).toMatchObject({
      key: "agent_1",
      name: "Reviewer",
      conversation_starters: [],
      max_concurrent_tasks: 1,
    });
  });
});
