import { describe, expect, it } from "vitest";
import { resolveEndpoints } from "./env";

describe("resolveEndpoints", () => {
  it("targets the local backend when nothing is configured", () => {
    expect(resolveEndpoints({})).toEqual({
      apiUrl: "http://localhost:8080",
      wsUrl: "ws://localhost:8080/ws",
      webUrl: "http://localhost:3000",
    });
  });

  it("derives a secure socket URL from an https API", () => {
    expect(
      resolveEndpoints({ apiUrl: "https://api.multica.ai/", webUrl: "https://multica.ai" }),
    ).toEqual({
      apiUrl: "https://api.multica.ai",
      wsUrl: "wss://api.multica.ai/ws",
      webUrl: "https://multica.ai",
    });
  });

  it("keeps an API mounted under a path", () => {
    expect(resolveEndpoints({ apiUrl: "https://example.com/multica" }).wsUrl).toBe(
      "wss://example.com/multica/ws",
    );
  });

  it("uses an explicit socket URL", () => {
    expect(
      resolveEndpoints({ apiUrl: "https://api.example.com", wsUrl: "wss://rt.example.com/ws" }).wsUrl,
    ).toBe("wss://rt.example.com/ws");
  });

  it("rejects a misconfigured URL at startup", () => {
    expect(() => resolveEndpoints({ apiUrl: "api.multica.ai" })).toThrow(/EXPO_PUBLIC_API_URL/);
    expect(() => resolveEndpoints({ apiUrl: "ftp://x" })).toThrow(/http or https/);
    expect(() => resolveEndpoints({ wsUrl: "https://x/ws" })).toThrow(/EXPO_PUBLIC_WS_URL/);
  });
});
