import { describe, expect, it } from "vitest";
import { runtimeProbeSignature } from "./client-usage-reporter";

describe("runtimeProbeSignature", () => {
  it("is stable across provider insertion order", () => {
    const base = {
      probeResult: "success" as const,
      runtimeCount: 2,
      onlineCount: 1,
      offlineCount: 1,
    };
    expect(
      runtimeProbeSignature({ ...base, providerSummary: { codex: 1, claude: 1 } }),
    ).toBe(
      runtimeProbeSignature({ ...base, providerSummary: { claude: 1, codex: 1 } }),
    );
  });

  it("distinguishes runtime state changes", () => {
    const base = {
      probeResult: "success" as const,
      runtimeCount: 1,
      providerSummary: { codex: 1 },
      offlineCount: 0,
    };
    expect(runtimeProbeSignature({ ...base, onlineCount: 1 })).not.toBe(
      runtimeProbeSignature({ ...base, onlineCount: 0, offlineCount: 1 }),
    );
  });
});
