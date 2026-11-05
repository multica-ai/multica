// @vitest-environment node
import { describe, expect, it } from "vitest";
import { codeGraphAssignmentChanges, type AssignmentOption } from "./codegraph-assignments";
import { AgentRuntimeSchema } from "../runtimes/schema";

describe("graph assignment changes", () => {
  const option = (id: string, assigned = false, editable = true, supported = true): AssignmentOption => ({ id, label: id, assigned, editable, supported });
  it("adds and removes several explicit choices without touching inaccessible bindings", () => {
    expect(codeGraphAssignmentChanges([option("keep", true), option("remove", true), option("new"), option("other-owner", true, false)], new Set(["keep", "new"])))
      .toEqual([{ id: "remove", assigned: false }, { id: "new", assigned: true }]);
  });
  it("allows cleanup of legacy external bindings but never adds unsupported targets", () => {
    expect(codeGraphAssignmentChanges([option("legacy", true, true, false), option("external", false, true, false)], new Set(["external"])))
      .toEqual([{ id: "legacy", assigned: false }]);
  });
  it("preserves assignments another client adds while the editor is open", () => {
    expect(codeGraphAssignmentChanges([option("concurrent", true), option("chosen")], new Set(["chosen"]), new Set(["chosen"])))
      .toEqual([{ id: "chosen", assigned: true }]);
  });
  it("does not repeat successful changes after the server refreshes a partially applied selection", () => {
    expect(codeGraphAssignmentChanges([option("success", true), option("retry")], new Set(["success", "retry"])))
      .toEqual([{ id: "retry", assigned: true }]);
  });
  it("does not treat an unknown or malformed runtime capability as support", () => {
    expect([undefined, "true"].map(value => AgentRuntimeSchema.shape.supports_code_graph.parse(value))).toEqual([false, false]);
  });
});
