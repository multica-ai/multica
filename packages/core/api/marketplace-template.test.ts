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

  it("exports and reapplies a squad template file through schema-checked endpoints", async () => {
    const manifest = {
      format: "multica-template",
      version: 1,
      exported_at: "2026-09-01T00:00:00Z",
      name: "Delivery squad",
      description: "A reusable delivery squad",
      tags: [],
      source_type: "squad",
      snapshot_version: 1,
      snapshot: {
        version: 1,
        source_type: "squad",
        agents: [{ key: "agent_1", name: "Lead", instructions: "Delegate", skill_keys: [] }],
        skills: [],
        squad: { name: "Delivery squad", leader_key: "agent_1", members: [{ agent_key: "agent_1", role: "leader" }] },
      },
    };
    stubJSON(manifest);
    const client = new ApiClient("https://api.example.test");

    const exported = await client.exportSquadTemplateFile("squad-1");
    expect(exported).toMatchObject({ format: "multica-template", source_type: "squad" });
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/squads/squad-1/template-file",
    );

    stubJSON({ template_id: "", agent_ids: { agent_1: "new-agent" }, squad_id: "new-squad", reused_skill_ids: [] });
    const applied = await client.applyMarketplaceTemplateFile({
      manifest: exported,
      name: "Imported squad",
      runtime_ids: { agent_1: "runtime-1" },
    });
    expect(applied).toMatchObject({ squad_id: "new-squad", template_id: "" });
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/templates/apply-file",
    );
  });
});
