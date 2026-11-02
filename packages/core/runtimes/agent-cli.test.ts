// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  agentCLIDisplayName,
  canUpgradeAgentCLI,
  isManualInstallOnly,
} from "./agent-cli";

describe("isManualInstallOnly", () => {
  it("is manual when the server says the row is not upgradable and not current", () => {
    for (const cli of ["codex", "claude", "grok"]) {
      expect(
        isManualInstallOnly({
          cli,
          current: "",
          latest: "",
          status: "missing",
          upgradable: false,
        }),
      ).toBe(true);
      expect(
        isManualInstallOnly({
          cli,
          current: "1.0.0",
          latest: "",
          status: "unknown",
          upgradable: false,
        }),
      ).toBe(true);
      expect(
        isManualInstallOnly({
          cli,
          current: "1.0.0",
          latest: "1.1.0",
          status: "outdated",
          upgradable: false,
        }),
      ).toBe(true);
    }
  });

  it("is not manual when the server marks the row upgradable", () => {
    expect(
      isManualInstallOnly({
        cli: "grok",
        current: "1.0.0",
        latest: "1.1.0",
        status: "outdated",
        upgradable: true,
      }),
    ).toBe(false);
    expect(
      isManualInstallOnly({
        cli: "codex",
        current: "1.0.0",
        latest: "1.1.0",
        status: "outdated",
        upgradable: true,
      }),
    ).toBe(false);
  });

  it("is not manual for an up-to-date row", () => {
    expect(
      isManualInstallOnly({
        cli: "claude",
        current: "1.0.0",
        latest: "1.0.0",
        status: "up_to_date",
        upgradable: false,
      }),
    ).toBe(false);
  });
});

describe("agentCLIDisplayName", () => {
  it("title-cases known CLIs", () => {
    expect(agentCLIDisplayName("codex")).toBe("Codex");
    expect(agentCLIDisplayName("claude")).toBe("Claude");
    expect(agentCLIDisplayName("grok")).toBe("Grok");
    expect(agentCLIDisplayName("other")).toBe("other");
  });
});

describe("canUpgradeAgentCLI", () => {
  it("allows only the bound owner on local runtimes", () => {
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "local",
        isRuntimeOwner: true,
        isWorkspaceAdmin: false,
      }),
    ).toBe(true);
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "local",
        isRuntimeOwner: true,
        isWorkspaceAdmin: true,
      }),
    ).toBe(true);
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "local",
        isRuntimeOwner: false,
        isWorkspaceAdmin: true,
      }),
    ).toBe(false);
  });

  it("allows a workspace admin or owner, or the runtime owner, on cloud", () => {
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "cloud",
        isRuntimeOwner: false,
        isWorkspaceAdmin: true,
      }),
    ).toBe(true);
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "cloud",
        isRuntimeOwner: true,
        isWorkspaceAdmin: false,
      }),
    ).toBe(true);
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "cloud",
        isRuntimeOwner: false,
        isWorkspaceAdmin: false,
      }),
    ).toBe(false);
  });

  it("treats an unknown mode like local", () => {
    expect(
      canUpgradeAgentCLI({
        runtimeMode: "remote",
        isRuntimeOwner: false,
        isWorkspaceAdmin: true,
      }),
    ).toBe(false);
  });
});
