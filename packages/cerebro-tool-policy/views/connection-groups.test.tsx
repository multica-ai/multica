import { describe, expect, it } from "vitest";

import { groupConnectionRows } from "./tool-policy-table";
import type { ToolPolicyRow } from "../core";

// FIR-1512 Workstream B — connection tools are governed UNDER their connection
// as a foldable tree. groupConnectionRows pairs each connection-wide row with
// its per-tool sub-rows, orders the tools, and narrows by the free-text search.

const connRow = (toolKey: string, title: string): ToolPolicyRow =>
  ({
    tool_key: toolKey,
    resource_pattern: "",
    title,
    category: title,
    source: "connection",
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
  }) as unknown as ToolPolicyRow;

const toolRow = (toolKey: string, pattern: string): ToolPolicyRow =>
  ({
    tool_key: toolKey,
    resource_pattern: pattern,
    title: pattern,
    category: "",
    source: "connection-tool",
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: { setting: "allow", decided_by: "", capped_by: "", reason: "" },
  }) as unknown as ToolPolicyRow;

describe("groupConnectionRows", () => {
  const cs = connRow("connection:customer-service", "Customer Service");
  const reg = connRow("connection:firtal-registry", "Firtal Registry");
  const toolsByKey = new Map<string, ToolPolicyRow[]>([
    [
      "connection:customer-service",
      [
        toolRow("connection:customer-service", "lookup_order"),
        toolRow("connection:customer-service", "get_order_confirmation"),
      ],
    ],
    ["connection:firtal-registry", [toolRow("connection:firtal-registry", "query_table")]],
  ]);

  it("nests each connection's tools under the connection, sorted by name", () => {
    const groups = groupConnectionRows([cs, reg], toolsByKey, "");
    // Connections sorted by label: "Customer Service" before "Firtal Registry".
    expect(groups.map((g) => g.conn.tool_key)).toEqual([
      "connection:customer-service",
      "connection:firtal-registry",
    ]);
    // Tools sorted by resource_pattern within the connection.
    expect(groups[0]!.tools.map((t) => t.resource_pattern)).toEqual([
      "get_order_confirmation",
      "lookup_order",
    ]);
  });

  it("keeps a connection that matches the search by its own name", () => {
    const groups = groupConnectionRows([cs, reg], toolsByKey, "registry");
    expect(groups.map((g) => g.conn.tool_key)).toEqual(["connection:firtal-registry"]);
  });

  it("keeps a connection when one of its tools matches the search", () => {
    const groups = groupConnectionRows([cs, reg], toolsByKey, "lookup_order");
    expect(groups.map((g) => g.conn.tool_key)).toEqual(["connection:customer-service"]);
    expect(groups[0]!.tools).toHaveLength(2);
  });

  it("returns a connection with an empty tool list when it has no sub-rows", () => {
    const groups = groupConnectionRows([cs], new Map(), "");
    expect(groups).toHaveLength(1);
    expect(groups[0]!.tools).toEqual([]);
  });
});
