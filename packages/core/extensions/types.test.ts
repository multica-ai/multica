import { describe, expect, it } from "vitest";
import {
  PLATFORM_EXTENSION_FLOW_COMMAND_SUFFIX,
  classifyPlatformExtensionCommand,
} from "./types";

describe("Platform Extension command classification", () => {
  it("uses the configurable -e2e suffix for Flow Commands", () => {
    expect(PLATFORM_EXTENSION_FLOW_COMMAND_SUFFIX).toBe("-e2e");
    expect(classifyPlatformExtensionCommand("delegate-e2e")).toBe("flow");
    expect(classifyPlatformExtensionCommand("delegate.flow")).toBe("skill");
    expect(classifyPlatformExtensionCommand("summarize")).toBe("skill");
  });
});
