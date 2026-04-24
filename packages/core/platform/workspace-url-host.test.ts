import { describe, it, expect, beforeEach } from "vitest";
import { getWorkspaceUrlHost, setWorkspaceUrlHost } from "./workspace-url-host";

describe("workspace-url-host", () => {
  beforeEach(() => {
    // Reset singleton to default between tests. The module keeps a
    // module-level binding, so tests would otherwise leak state.
    setWorkspaceUrlHost(undefined);
  });

  it("returns the default host 'multica.ai' when nothing is set", () => {
    expect(getWorkspaceUrlHost()).toBe("multica.ai");
  });

  it("returns the default when setter is called with undefined", () => {
    setWorkspaceUrlHost(undefined);
    expect(getWorkspaceUrlHost()).toBe("multica.ai");
  });

  it("returns the default when setter is called with empty string", () => {
    // Empty string must not override the default — otherwise an unset
    // NEXT_PUBLIC_*/VITE_* env var (which Next/Vite can inline as "")
    // would silently erase the fallback brand.
    setWorkspaceUrlHost("");
    expect(getWorkspaceUrlHost()).toBe("multica.ai");
  });

  it("returns the configured host when setter is called with a non-empty value", () => {
    setWorkspaceUrlHost("agentfarm.g2.com");
    expect(getWorkspaceUrlHost()).toBe("agentfarm.g2.com");
  });

  it("replaces the previously-set host on each call (last write wins)", () => {
    setWorkspaceUrlHost("first.example.com");
    setWorkspaceUrlHost("second.example.com");
    expect(getWorkspaceUrlHost()).toBe("second.example.com");
  });

  it("falls back to default when setter is called with undefined after a value", () => {
    setWorkspaceUrlHost("agentfarm.g2.com");
    setWorkspaceUrlHost(undefined);
    expect(getWorkspaceUrlHost()).toBe("multica.ai");
  });
});
