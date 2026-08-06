import { describe, expect, it } from "vitest";

import {
  CompanyBrainMigrationCensusSchema,
  ConnectionSchema,
  TestResultSchema,
} from "./queries";

const base = {
  id: "c1", workspace_id: "w1", name: "infisical-admin", display_name: "Infisical (admin)",
  type: "api", url: "http://internal:8080", created_at: "2026-06-29T00:00:00Z", updated_at: "2026-06-29T00:00:00Z",
};

// FIR-2166 "C" v2: default_access is a new connection field. Per CLAUDE.md "API
// Response Compatibility", unknown/missing enum values must downgrade to the safe
// "deny", never throw into the UI.
describe("ConnectionSchema — default_access", () => {
  it("parses a valid default_access value", () => {
    expect(ConnectionSchema.parse({ ...base, default_access: "allow" }).default_access).toBe("allow");
  });

  it("downgrades an unknown default_access to deny", () => {
    expect(ConnectionSchema.parse({ ...base, default_access: "wide-open" }).default_access).toBe("deny");
  });

  it("defaults a missing default_access to deny", () => {
    expect(ConnectionSchema.parse(base).default_access).toBe("deny");
  });
});

describe("ConnectionSchema — instructions", () => {
  it("preserves agent-facing connection instructions", () => {
    expect(ConnectionSchema.parse({ ...base, instructions: "Search before answering." }).instructions).toBe("Search before answering.");
  });

  it("defaults missing instructions to an empty string", () => {
    expect(ConnectionSchema.parse(base).instructions).toBe("");
  });
});

// TECH-3410: the connection test result now carries discovered API endpoints.
// Per CLAUDE.md "API Response Compatibility", the schema must survive drift —
// missing, extra, and wrong-typed fields must not throw into the UI.
describe("TestResultSchema — endpoints", () => {
  it("parses a result with discovered endpoints", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      status_code: 200,
      endpoints: [
        { path: "/orders", methods: ["GET", "POST"] },
        { path: "/orders/{id}", methods: ["GET", "DELETE"] },
      ],
    });
    expect(parsed.reachable).toBe(true);
    expect(parsed.endpoints).toHaveLength(2);
    expect(parsed.endpoints?.[0]?.methods).toEqual(["GET", "POST"]);
  });

  it("defaults methods to [] when the server omits them", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      endpoints: [{ path: "/health" }],
    });
    expect(parsed.endpoints?.[0]?.methods).toEqual([]);
  });

  it("tolerates a result with no endpoints field (MCP connection)", () => {
    const parsed = TestResultSchema.parse({ reachable: true, tools: [{ name: "draft_reply" }] });
    expect(parsed.endpoints).toBeUndefined();
    expect(parsed.tools).toHaveLength(1);
  });

  it("rejects a malformed endpoints entry so parseWithFallback can fall back", () => {
    // A path that is not a string is a contract violation — the schema must
    // throw here (parseWithFallback catches it upstream and returns the
    // fallback rather than white-screening).
    expect(() =>
      TestResultSchema.parse({ reachable: true, endpoints: [{ path: 123, methods: ["GET"] }] }),
    ).toThrow();
  });
});

// Discovered endpoints now carry the optional one-line OpenAPI summary. Older
// servers omit it; the schema must accept both shapes.
describe("TestResultSchema — endpoint summary", () => {
  it("parses an endpoint with a summary", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      endpoints: [
        { path: "/data-sources/9be2/execute", methods: ["POST"], summary: "Execute data source: Orders" },
      ],
    });
    expect(parsed.endpoints?.[0]?.summary).toBe("Execute data source: Orders");
  });

  it("accepts an endpoint without a summary", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      endpoints: [{ path: "/manifest", methods: ["GET"] }],
    });
    expect(parsed.endpoints?.[0]?.summary).toBeUndefined();
  });
});

describe("TestResultSchema — scope suggestions", () => {
  it("parses advisory scopes returned by MCP discovery", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      tools: [{ name: "query_run" }, { name: "data_sources_list" }],
      scope_suggestions: [{
        tool: "query_run",
        arg: "data_source_id",
        options_source_tool: "data_sources_list",
        label: "Data source",
      }],
    });

    expect(parsed.scope_suggestions).toEqual([expect.objectContaining({
      tool: "query_run",
      arg: "data_source_id",
      options_source_tool: "data_sources_list",
    })]);
  });
});

describe("TestResultSchema — access diagnostics", () => {
  it("preserves the MCP discovery version and recovery contract", () => {
    const parsed = TestResultSchema.parse({
      reachable: true,
      diagnostics: [{
        code: "connection_discovery",
        state: "success",
        title: "MCP discovery",
        message: "Discovery returned 2 capabilities.",
        affected_capability: "mcp:*",
        source_policy: "MCP tools/list",
        recovery_action: "Retest after changing the Connection identity.",
        version: "sha256:abc123",
      }],
    });

    expect(parsed.diagnostics[0]).toEqual(expect.objectContaining({
      source_policy: "MCP tools/list",
      version: "sha256:abc123",
    }));
  });
});

describe("CompanyBrainMigrationCensusSchema", () => {
  const source = {
    connection_id: "c1",
    connection_name: "company-brain-commercial",
    status: "verified",
    claim: { write_source: "commercial", allowed_read_sources: ["commercial", "shared"] },
  };

  it("parses actor and automatic-run evidence", () => {
    const parsed = CompanyBrainMigrationCensusSchema.parse({
      generated_at: "2026-07-29T09:00:00Z",
      actors: [{ agent_id: "a1", name: "Lone", status: "online", sources: [source] }],
      automations: [{
        automation_id: "p1",
        title: "Daily brief",
        status: "active",
        assignee_type: "agent",
        assignee_id: "a1",
        trigger_kinds: ["schedule"],
        sources: [source],
      }],
      connections: [source],
    });
    expect(parsed.automations[0]?.trigger_kinds).toEqual(["schedule"]);
    expect(parsed.actors[0]?.sources[0]?.claim?.write_source).toBe("commercial");
  });

  it("rejects a malformed source identifier so callers can fall back safely", () => {
    expect(CompanyBrainMigrationCensusSchema.safeParse({
      generated_at: "2026-07-29T09:00:00Z",
      actors: [],
      automations: [],
      connections: [{ ...source, connection_id: 42 }],
    }).success).toBe(false);
  });

  it("downgrades an unknown verification status to unverifiable", () => {
    const parsed = CompanyBrainMigrationCensusSchema.parse({
      generated_at: "2026-07-29T09:00:00Z",
      actors: [],
      automations: [],
      connections: [{ ...source, status: "new-server-status" }],
    });
    expect(parsed.connections[0]?.status).toBe("unverifiable");
  });
});
