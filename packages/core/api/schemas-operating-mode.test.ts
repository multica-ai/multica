import { describe, expect, it } from "vitest";
import { StoredAgentDraftSchema } from "./schemas";

describe("stored agent draft operating mode", () => {
  it("preserves supported values", () => {
    expect(
      StoredAgentDraftSchema.parse({ operating_mode: "hybrid" }).operating_mode,
    ).toBe("hybrid");
  });

  it("defaults missing and invalid historical values to coding", () => {
    expect(StoredAgentDraftSchema.parse({}).operating_mode).toBe("coding");
    expect(
      StoredAgentDraftSchema.parse({ operating_mode: "admin" }).operating_mode,
    ).toBe("coding");
  });
});
