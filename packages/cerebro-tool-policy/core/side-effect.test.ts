import { describe, expect, it } from "vitest";
import { classFacets, classifySideEffect, type SideEffect } from "./side-effect";
import type { ToolPolicyRow, ToolEffectiveSetting } from "./tool-policy";

function row(
  tool_key: string,
  opts: { title?: string; category?: string; effective?: ToolEffectiveSetting } = {},
): ToolPolicyRow {
  return {
    tool_key,
    resource_pattern: "",
    title: opts.title ?? "",
    category: opts.category ?? "",
    source: "scan",
    managed_externally: false,
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: {
      setting: opts.effective ?? "allow",
      decided_by: "",
      capped_by: "",
      reason: "",
    },
    capped_by_groups: [],
  };
}

describe("classifySideEffect", () => {
  const cases: [string, SideEffect][] = [
    ["Bash", "write_local"],
    ["Edit", "write_local"],
    ["Write", "write_local"],
    ["deploy_restart", "write_local"],
    ["Read", "read"],
    ["TaskGet", "read"],
    ["list_issues", "read"],
    ["WebFetch", "egress"],
    ["WebSearch", "egress"],
    ["firtal_bq_query", "read"],
    ["gmail_send", "write_external"],
    ["slack.post_message", "write_external"],
    ["Agent", "control"],
    ["AskUserQuestion", "control"],
    ["EnterPlanMode", "control"],
  ];

  it.each(cases)("classifies %s as %s", (key, expected) => {
    expect(classifySideEffect(row(key))).toBe(expected);
  });

  it("classifies an OAuth connect flow as auth, ahead of any write keyword", () => {
    expect(classifySideEffect(row("connect_account", { title: "Connect account (OAuth login)" }))).toBe(
      "auth",
    );
  });

  it("falls back to control for an unrecognised agentic tool", () => {
    expect(classifySideEffect(row("Frobnicate"))).toBe("control");
  });

  it("matches keywords found only in the title or category", () => {
    expect(classifySideEffect(row("x1", { category: "Web access" }))).toBe("egress");
  });
});

describe("classFacets", () => {
  it("groups by category, counts, and tallies the verdict spread, preserving order", () => {
    const rows = [
      row("a", { category: "Built-in tools", effective: "allow" }),
      row("b", { category: "Built-in tools", effective: "deny" }),
      row("c", { category: "MCP · Gmail", effective: "ask" }),
    ];
    const facets = classFacets(rows);
    expect(facets.map((f) => f.category)).toEqual(["Built-in tools", "MCP · Gmail"]);
    expect(facets[0]).toMatchObject({ count: 2, allow: 1, ask: 0, deny: 1 });
    expect(facets[1]).toMatchObject({ count: 1, ask: 1 });
  });

  it("collapses an empty category to Uncategorised", () => {
    expect(classFacets([row("a")])[0]!.category).toBe("Uncategorised");
  });
});
