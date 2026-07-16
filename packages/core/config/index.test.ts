import { describe, it, expect, beforeEach } from "vitest";
import { configStore, officialBaseline } from "./index";

describe("officialBaseline", () => {
  it("returns the tag only for a clean v… official release", () => {
    expect(officialBaseline("v0.4.0")).toBe("v0.4.0");
  });
  it("rejects dev, empty, and undefined", () => {
    expect(officialBaseline("dev")).toBe("");
    expect(officialBaseline("")).toBe("");
    expect(officialBaseline(undefined)).toBe("");
  });
  it("rejects git-describe commit-distance suffixes", () => {
    expect(officialBaseline("v0.4.0-5-gabc1234")).toBe("");
    expect(officialBaseline("v0.4.0-rc1-3-gabc1234")).toBe("");
  });
  it("rejects the dirty marker", () => {
    expect(officialBaseline("v0.4.0-dirty")).toBe("");
  });
  it("rejects hashes and non-v values", () => {
    expect(officialBaseline("abc1234")).toBe("");
    expect(officialBaseline("garbage")).toBe("");
    expect(officialBaseline("1.2.3")).toBe("");
  });
  it("accepts a prerelease tag with no describe suffix", () => {
    expect(officialBaseline("v0.4.0-rc1")).toBe("v0.4.0-rc1");
  });
  it("trims surrounding whitespace", () => {
    expect(officialBaseline(" v0.4.0 ")).toBe("v0.4.0");
  });
});

describe("configStore build-provenance state", () => {
  beforeEach(() => {
    // Reset only the fields the provenance flow owns so unrelated store
    // state (featureFlags, auth, …) doesn't leak between tests.
    configStore.setState({
      frontendBaseline: "",
      backendBaseline: "",
      backendBaselineStatus: "loading",
      // serverVersion removed in U4 (replaced by frontendBaseline/backendBaseline)
    });
  });

  it("starts with backend baseline loading and both values unavailable", () => {
    expect(configStore.getState().frontendBaseline).toBe("");
    expect(configStore.getState().backendBaseline).toBe("");
    expect(configStore.getState().backendBaselineStatus).toBe("loading");
  });

  it("setFrontendBaseline stores a clean tag as the frontend baseline", () => {
    configStore.getState().setFrontendBaseline("v0.4.2");
    expect(configStore.getState().frontendBaseline).toBe("v0.4.2");
  });

  it("setFrontendBaseline rejects dev/non-tag values so a package.json or dev fallback never becomes a baseline", () => {
    configStore.getState().setFrontendBaseline("dev");
    expect(configStore.getState().frontendBaseline).toBe("");
    configStore.getState().setFrontendBaseline("0.1.0");
    expect(configStore.getState().frontendBaseline).toBe("");
    configStore.getState().setFrontendBaseline("v0.4.0-5-gabc1234");
    expect(configStore.getState().frontendBaseline).toBe("");
  });

  it("setBackendBaseline stores the normalized value and transitions loading -> settled", () => {
    configStore.getState().setBackendBaseline("v0.4.2");
    expect(configStore.getState().backendBaseline).toBe("v0.4.2");
    expect(configStore.getState().backendBaselineStatus).toBe("settled");
  });

  it("setBackendBaseline called with no value marks settled and unavailable (fetch failure path)", () => {
    configStore.setState({ backendBaselineStatus: "loading" });
    configStore.getState().setBackendBaseline();
    expect(configStore.getState().backendBaseline).toBe("");
    expect(configStore.getState().backendBaselineStatus).toBe("settled");
  });

  it("setBackendBaseline rejects non-baseline values from a buggy server", () => {
    configStore.getState().setBackendBaseline("v0.4.2-5-gabc1234");
    expect(configStore.getState().backendBaseline).toBe("");
    expect(configStore.getState().backendBaselineStatus).toBe("settled");
  });

  it("partial deployment: frontend and backend can differ", () => {
    configStore.getState().setFrontendBaseline("v0.4.2");
    configStore.getState().setBackendBaseline("v0.4.1");
    expect(configStore.getState().frontendBaseline).toBe("v0.4.2");
    expect(configStore.getState().backendBaseline).toBe("v0.4.1");
    expect(configStore.getState().backendBaselineStatus).toBe("settled");
  });
});