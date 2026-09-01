// @vitest-environment node

import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { MarketplaceTemplateFileSchema } from "./schemas";
import { parseMarketplaceTemplateFile } from "../templates/file";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubJSON(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  })));
}

function loadV2Fixture(): unknown {
  return JSON.parse(
    readFileSync(new URL("./testdata/squad-template-v2.json", import.meta.url), "utf8"),
  );
}

describe("marketplace template API", () => {
  it("accepts the deployed v2 squad template envelope", () => {
    const raw = loadV2Fixture();
    expect(MarketplaceTemplateFileSchema.safeParse(raw).success).toBe(true);
    const normalized = parseMarketplaceTemplateFile(raw);
    expect(normalized.success).toBe(true);
    if (!normalized.success) return;
    expect(normalized.file.snapshot.agents).toHaveLength(2);
    expect(normalized.file.snapshot.skills).toHaveLength(1);
    expect(normalized.file.snapshot.agents[0]?.skill_keys).toEqual(["review"]);
    expect(normalized.file.snapshot.squad).toMatchObject({
      leader_key: "lead",
      members: [
        { agent_key: "lead", role: "leader" },
        { agent_key: "worker", role: "implementation" },
      ],
    });
  });

  it("keeps accepting legacy v1 template files", () => {
    const normalized = parseMarketplaceTemplateFile({
      format: "multica-template",
      version: 1,
      name: "Legacy agent",
      description: "Exported before schema v2",
      tags: [],
      source_type: "agent",
      snapshot_version: 1,
      snapshot: {
        version: 1,
        source_type: "agent",
        agents: [
          {
            key: "agent_1",
            name: "Legacy agent",
            instructions: "Help",
            skill_keys: [],
          },
        ],
        skills: [],
      },
    });
    expect(normalized.success).toBe(true);
    if (!normalized.success) return;
    expect(normalized.file.snapshot.agents[0]?.name).toBe("Legacy agent");
  });

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
    const manifest = loadV2Fixture();
    stubJSON(manifest);
    const client = new ApiClient("https://api.example.test");

    const exported = await client.exportSquadTemplateFile("squad-1");
    expect(exported).toMatchObject({ format: "multica.template", schema_version: 2, type: "squad" });
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
