import { describe, expect, it } from "vitest";
import { conditionIsEmpty, summarizeCondition } from "./tool-policy-table";
import { GROUP_ARG } from "./group-scope";
import type { ToolCondition } from "../core/tool-policy";

const base: ToolCondition = { host_allowlist: [], actions: [], arg_allowlist: [], expr: "" };

describe("conditionIsEmpty with arg_allowlist", () => {
  it("treats an arg allowlist with values as non-empty", () => {
    expect(
      conditionIsEmpty({ ...base, arg_allowlist: [{ arg: "data_source_id", values: ["a"] }] }),
    ).toBe(false);
  });

  it("treats an inert (empty values) arg entry as empty", () => {
    expect(
      conditionIsEmpty({ ...base, arg_allowlist: [{ arg: "data_source_id", values: [] }] }),
    ).toBe(true);
  });

  it("treats a group allowlist with values as non-empty", () => {
    expect(
      conditionIsEmpty({ ...base, arg_allowlist: [{ arg: GROUP_ARG, values: ["g1"] }] }),
    ).toBe(false);
  });
});

describe("summarizeCondition with arg_allowlist", () => {
  it("summarizes a folder scope", () => {
    expect(
      summarizeCondition({ ...base, arg_allowlist: [{ arg: "folder_id", values: ["f1", "f2"] }] }),
    ).toBe("2 folders");
  });

  it("summarizes a single-source scope", () => {
    expect(
      summarizeCondition({ ...base, arg_allowlist: [{ arg: "data_source_id", values: ["a"] }] }),
    ).toBe("1 source");
  });

  it("summarizes a single-group scope", () => {
    expect(
      summarizeCondition({ ...base, arg_allowlist: [{ arg: GROUP_ARG, values: ["g1"] }] }),
    ).toBe("1 group");
  });

  it("summarizes a multi-group scope", () => {
    expect(
      summarizeCondition({ ...base, arg_allowlist: [{ arg: GROUP_ARG, values: ["g1", "g2"] }] }),
    ).toBe("2 groups");
  });

  it("combines arg with other terms", () => {
    const s = summarizeCondition({
      ...base,
      actions: ["execute"],
      arg_allowlist: [{ arg: "data_source_id", values: ["a", "b", "c"] }],
    });
    expect(s).toBe("execute · 3 sources");
  });
});
