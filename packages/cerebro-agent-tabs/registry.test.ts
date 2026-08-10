import { describe, expect, it } from "vitest";
import { agentDetailTabExtensions } from "./index";

const ids = (flags: { productionPromptOn: boolean; qualityOn: boolean }) =>
  agentDetailTabExtensions(flags).map((tab) => tab.id);

describe("agentDetailTabExtensions", () => {
  it("always mounts the unflagged tabs", () => {
    expect(ids({ productionPromptOn: false, qualityOn: false })).toEqual([
      "tools",
      "capabilities",
      "memory",
    ]);
  });

  it("mounts each flagged tab only when its flag is on", () => {
    expect(ids({ productionPromptOn: true, qualityOn: false })).toContain(
      "production_prompt",
    );
    expect(ids({ productionPromptOn: false, qualityOn: true })).toContain(
      "quality",
    );
    expect(ids({ productionPromptOn: true, qualityOn: true })).toEqual([
      "tools",
      "capabilities",
      "memory",
      "production_prompt",
      "quality",
    ]);
  });

  it("gives every tab a unique id and a label key", () => {
    const tabs = agentDetailTabExtensions({
      productionPromptOn: true,
      qualityOn: true,
    });
    expect(new Set(tabs.map((tab) => tab.id)).size).toBe(tabs.length);
    for (const tab of tabs) expect(tab.labelKey).toBeTruthy();
  });
});
