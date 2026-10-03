// @vitest-environment node

import { describe, expect, it } from "vitest";
import { localDevTokenForApi } from "./local-dev-auth";

describe("localDevTokenForApi", () => {
  it.each([
    "http://localhost:8080",
    "http://127.0.0.1:18080",
    "http://[::1]:8080",
  ])("allows a local token for loopback API %s", (apiUrl) => {
    expect(localDevTokenForApi(apiUrl, " local-token ")).toBe("local-token");
  });

  it.each([
    "https://localhost:8080",
    "https://api.multica.ai",
    "http://192.168.1.20:8080",
    "not-a-url",
  ])("rejects a local token for non-local API %s", (apiUrl) => {
    expect(localDevTokenForApi(apiUrl, "local-token")).toBeNull();
  });

  it("rejects a missing token", () => {
    expect(localDevTokenForApi("http://localhost:8080", undefined)).toBeNull();
  });
});
