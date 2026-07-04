import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types/agent";
import { matchesRuntimeSearch } from "./runtime-search";

// Minimal runtime fixture — only the fields matchesRuntimeSearch reads.
function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    name: "Claude (mac-mini)",
    provider: "claude",
    device_info: "Darwin arm64",
    runtime_mode: "local",
    metadata: { version: "2.1.199 (Claude Code)", cli_version: "0.5.0" },
    ...overrides,
  } as unknown as AgentRuntime;
}

describe("matchesRuntimeSearch", () => {
  it("matches everything on an empty query", () => {
    expect(matchesRuntimeSearch(makeRuntime(), "")).toBe(true);
    expect(matchesRuntimeSearch(makeRuntime(), "   ")).toBe(true);
  });

  it("matches the runtime name (case-insensitive)", () => {
    expect(matchesRuntimeSearch(makeRuntime(), "CLAUDE")).toBe(true);
    expect(matchesRuntimeSearch(makeRuntime(), "mac-mini")).toBe(true);
  });

  it("matches the provider and device info", () => {
    expect(matchesRuntimeSearch(makeRuntime(), "darwin")).toBe(true);
  });

  it("matches the agent-native CLI version from metadata", () => {
    expect(matchesRuntimeSearch(makeRuntime(), "2.1.199")).toBe(true);
    expect(matchesRuntimeSearch(makeRuntime(), "0.5.0")).toBe(true);
  });

  it("matches the resolved account label passed as an extra", () => {
    expect(
      matchesRuntimeSearch(makeRuntime(), "billing@firtal", {
        accountLabel: "billing@firtal.dk claude",
      }),
    ).toBe(true);
  });

  it("matches the owner name and health label extras", () => {
    expect(
      matchesRuntimeSearch(makeRuntime(), "jesper", {
        ownerName: "Jesper Hvejsel",
      }),
    ).toBe(true);
    expect(
      matchesRuntimeSearch(makeRuntime(), "offline", {
        healthLabel: "Offline",
      }),
    ).toBe(true);
  });

  it("returns false when nothing matches", () => {
    expect(matchesRuntimeSearch(makeRuntime(), "nonexistent-token")).toBe(
      false,
    );
  });

  it("does not throw when metadata is null", () => {
    const rt = makeRuntime({ metadata: null as unknown as AgentRuntime["metadata"] });
    expect(matchesRuntimeSearch(rt, "claude")).toBe(true);
    expect(matchesRuntimeSearch(rt, "2.1.199")).toBe(false);
  });
});
