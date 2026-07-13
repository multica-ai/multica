import { describe, expect, it } from "vitest";
import { conditionFacets } from "./condition-facets";
import type { ToolPolicyRow } from "./tool-policy";

function row(over: Partial<ToolPolicyRow> = {}): ToolPolicyRow {
  return {
    tool_key: "x",
    resource_pattern: "",
    title: "",
    category: "",
    source: "scan",
    managed_externally: false,
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null, system: null },
    conditions: { workspace: null, runtime: null, agent: null, user: null, system: null },
    effective: { setting: "allow", decided_by: "", capped_by: "", reason: "", openable: false },
    capped_by_groups: [],
    ...over,
  };
}

describe("conditionFacets — actions preset", () => {
  it("returns get_schema/execute for the firtal_registry capability key", () => {
    expect(conditionFacets(row({ tool_key: "firtal_registry" })).actions).toEqual([
      "get_schema",
      "execute",
    ]);
  });

  it("returns get_schema/execute for a registry-data-source sub-row", () => {
    expect(
      conditionFacets(row({ tool_key: "firtal_registry", source: "registry-data-source" })).actions,
    ).toEqual(["get_schema", "execute"]);
  });

  it("returns read/checkout/push when the tool key starts with repo.", () => {
    expect(conditionFacets(row({ tool_key: "repo.read" })).actions).toEqual([
      "read",
      "checkout",
      "push",
    ]);
  });

  it("returns read/checkout/push for the repo category", () => {
    expect(conditionFacets(row({ tool_key: "x", category: "repo" })).actions).toEqual([
      "read",
      "checkout",
      "push",
    ]);
  });

  it("returns read/checkout/push for the repo source", () => {
    expect(conditionFacets(row({ tool_key: "x", source: "repo" })).actions).toEqual([
      "read",
      "checkout",
      "push",
    ]);
  });

  it("returns reveal/rotate when the tool key mentions a credential", () => {
    expect(conditionFacets(row({ tool_key: "reveal_credential" })).actions).toEqual([
      "reveal",
      "rotate",
    ]);
  });

  it("returns no actions for a plain tool with no verb dimension", () => {
    expect(conditionFacets(row({ tool_key: "send_notification" })).actions).toEqual([]);
  });
});

describe("conditionFacets — host facet", () => {
  it("is true for an egress side effect", () => {
    // "download" matches the egress matcher in classifySideEffect.
    expect(conditionFacets(row({ tool_key: "download_report" })).host).toBe(true);
  });

  it("is true for the web_fetch built-in", () => {
    expect(conditionFacets(row({ tool_key: "web_fetch" })).host).toBe(true);
  });

  it("is true for the web_search built-in", () => {
    expect(conditionFacets(row({ tool_key: "web_search" })).host).toBe(true);
  });

  it("is true for the WebFetch camelCase built-in", () => {
    expect(conditionFacets(row({ tool_key: "WebFetch" })).host).toBe(true);
  });

  it("is true for a connection capability row", () => {
    expect(conditionFacets(row({ tool_key: "connection:slack", source: "connection" })).host).toBe(
      true,
    );
  });

  it("is true for a connection-tool sub-row", () => {
    expect(
      conditionFacets(row({ tool_key: "connection:slack", source: "connection-tool" })).host,
    ).toBe(true);
  });

  it("is true for a connection-endpoint sub-row", () => {
    expect(
      conditionFacets(row({ tool_key: "connection:api", source: "connection-endpoint" })).host,
    ).toBe(true);
  });

  it("is true for a connection: prefixed key even on another source", () => {
    expect(conditionFacets(row({ tool_key: "connection:custom", source: "scan" })).host).toBe(true);
  });

  it("is false for a non-host, non-egress tool", () => {
    // A notification tool — the CEO's example of where host is nonsense.
    expect(conditionFacets(row({ tool_key: "send_notification" })).host).toBe(false);
  });

  it("is false for a plain read tool", () => {
    expect(conditionFacets(row({ tool_key: "list_issues", category: "Built-in tools" })).host).toBe(
      false,
    );
  });
});

describe("conditionFacets — combined meaningfulness", () => {
  it("repo rows are meaningful via actions even without a host", () => {
    const f = conditionFacets(row({ tool_key: "repo.push" }));
    expect(f.host).toBe(false);
    expect(f.actions.length).toBeGreaterThan(0);
  });

  it("web_fetch is meaningful via host even without actions", () => {
    const f = conditionFacets(row({ tool_key: "web_fetch" }));
    expect(f.host).toBe(true);
    expect(f.actions).toEqual([]);
  });

  it("a notification tool is meaningful on neither facet", () => {
    const f = conditionFacets(row({ tool_key: "send_notification" }));
    expect(f.host).toBe(false);
    expect(f.actions).toEqual([]);
  });
});
