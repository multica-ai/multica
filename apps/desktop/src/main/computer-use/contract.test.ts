// @vitest-environment node
import { describe, expect, it } from "vitest";
import contract from "@multica/computer-use/contract/computer.json";
import { computerPrompts } from "@multica/computer-use/prompts";
import { actionSchema, envelopeSchema } from "./broker";

describe("Computer contract", () => {
  it("matches the broker's envelope and actions", () => {
    expect(Object.keys(envelopeSchema.shape).sort()).toEqual([...contract.control.envelope].sort());
    expect(Object.keys(envelopeSchema.shape.scope.shape).sort()).toEqual([...contract.control.scope].sort());
    expect([...envelopeSchema.shape.op.options].sort()).toEqual([...contract.control.ops].sort());
    expect(actionSchema.options.map((option) => option.shape.op.value).sort()).toEqual([...contract.control.actions].sort());
  });
  it("reports every prompt the contract names, naming the task server", () => {
    expect(Object.keys(computerPrompts).sort()).toEqual([...contract.initialize.prompts].sort());
    expect(computerPrompts.taskBrief).toContain(`\`${contract.taskServerName}\``);
  });
});
