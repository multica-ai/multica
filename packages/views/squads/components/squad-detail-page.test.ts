import { describe, expect, it } from "vitest";
import { displayExtensionInternalAgentName } from "./squad-detail-page";

describe("displayExtensionInternalAgentName", () => {
  it("shows only the Agent name for a versioned Extension resource", () => {
    expect(displayExtensionInternalAgentName("Runtime Pool Demo v1.0.0 / Pool Reviewer"))
      .toBe("Pool Reviewer");
  });
});
