// @vitest-environment node
import { describe, expect, it } from "vitest";
import { BUILD_SERVER_URL, BUILD_UPDATE_URL, resolveUpdateFeed } from "./build-config";

describe("build config", () => {
  it("is empty when the bundle was not built with the env vars", () => {
    expect(BUILD_SERVER_URL).toBe("");
    expect(BUILD_UPDATE_URL).toBe("");
    expect(resolveUpdateFeed()).toEqual({ kind: "default" });
  });

  it("keeps the official feed when neither URL is set", () => {
    expect(resolveUpdateFeed({ serverUrl: "", updateUrl: "" })).toEqual({
      kind: "default",
    });
  });

  it("disables updates for a self-hosted build without an update URL", () => {
    expect(
      resolveUpdateFeed({ serverUrl: "https://multica.example.com", updateUrl: "" }),
    ).toEqual({ kind: "disabled" });
  });

  it("uses the generic feed whenever an update URL is set", () => {
    const url = "https://multica.example.com/desktop/";
    expect(
      resolveUpdateFeed({ serverUrl: "https://multica.example.com", updateUrl: url }),
    ).toEqual({ kind: "generic", url });
    expect(resolveUpdateFeed({ serverUrl: "", updateUrl: url })).toEqual({
      kind: "generic",
      url,
    });
  });
});
