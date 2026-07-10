import { describe, expect, it } from "vitest";
import type { AgentRuntime } from "@multica/core/types";
import { daemonVersion, runtimeVersion } from "./runtime-meta";

function runtimeWith(metadata: Record<string, unknown>): AgentRuntime {
  // Only `metadata` matters for runtimeVersion; the rest is filler to satisfy
  // the type without pretending the other fields carry meaning here.
  return { metadata } as unknown as AgentRuntime;
}

describe("runtimeVersion", () => {
  it("returns the `version` key when present", () => {
    expect(runtimeVersion(runtimeWith({ version: "cerebro v2.14.0" }))).toBe(
      "cerebro v2.14.0",
    );
  });

  it("falls back to `cli_version` when `version` is absent", () => {
    expect(runtimeVersion(runtimeWith({ cli_version: "0.5.0" }))).toBe("0.5.0");
  });

  it("prefers `version` over `cli_version`", () => {
    expect(
      runtimeVersion(runtimeWith({ version: "2.1.0", cli_version: "0.5.0" })),
    ).toBe("2.1.0");
  });

  it("returns null for a null runtime", () => {
    expect(runtimeVersion(null)).toBeNull();
  });

  it("returns null when neither key is a non-empty string", () => {
    expect(runtimeVersion(runtimeWith({}))).toBeNull();
    expect(runtimeVersion(runtimeWith({ version: "" }))).toBeNull();
    expect(runtimeVersion(runtimeWith({ version: 42 }))).toBeNull();
  });
});

describe("daemonVersion", () => {
  it("returns the daemon cli_version rather than the agent tool version", () => {
    expect(
      daemonVersion(
        runtimeWith({ version: "codex-cli 0.118.0", cli_version: "0.6.1" }),
      ),
    ).toBe("0.6.1");
  });

  it("returns null when cli_version is missing or invalid", () => {
    expect(daemonVersion(runtimeWith({ version: "codex-cli 0.118.0" }))).toBeNull();
    expect(daemonVersion(runtimeWith({ cli_version: "" }))).toBeNull();
    expect(daemonVersion(runtimeWith({ cli_version: 7 }))).toBeNull();
    expect(daemonVersion(null)).toBeNull();
  });
});
