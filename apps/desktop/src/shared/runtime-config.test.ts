import { describe, expect, it } from "vitest";
import {
  DEFAULT_RUNTIME_CONFIG,
  deriveWsUrl,
  parseRuntimeConfig,
  runtimeConfigFromDevEnv,
} from "./runtime-config";

describe("runtime config", () => {
  it("uses cloud defaults without a desktop.json file", () => {
    expect(DEFAULT_RUNTIME_CONFIG).toEqual({
      schemaVersion: 1,
      apiUrl: "https://api.multica.ai",
      wsUrl: "wss://api.multica.ai/ws",
      appUrl: "https://multica.ai",
    });
  });

  it("derives https/wss compatible URLs from apiUrl", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: "https://congvc-x99.taila6fa8a.ts.net:18443",
        }),
      ),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: "https://congvc-x99.taila6fa8a.ts.net:18443",
      wsUrl: "wss://congvc-x99.taila6fa8a.ts.net:18443/ws",
      appUrl: "https://congvc-x99.taila6fa8a.ts.net:18443",
    });
  });

  it("derives ws for http api URLs", () => {
    expect(deriveWsUrl("http://localhost:8080")).toBe("ws://localhost:8080/ws");
  });

  it("accepts explicit appUrl and wsUrl", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: "https://api.example.com/",
          wsUrl: "wss://ws.example.com/socket/",
          appUrl: "https://app.example.com/",
        }),
      ),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: "https://api.example.com",
      wsUrl: "wss://ws.example.com/socket",
      appUrl: "https://app.example.com",
    });
  });

  it("rejects invalid JSON", () => {
    expect(() => parseRuntimeConfig("{")).toThrow(/Invalid desktop runtime config JSON/);
  });

  it("rejects unsupported schema versions", () => {
    expect(() =>
      parseRuntimeConfig(JSON.stringify({ schemaVersion: 2, apiUrl: "https://api.example.com" })),
    ).toThrow(/schemaVersion/);
  });

  it("rejects non-http api schemes", () => {
    expect(() =>
      parseRuntimeConfig(JSON.stringify({ schemaVersion: 1, apiUrl: "file:///tmp/multica" })),
    ).toThrow(/apiUrl must use http or https/);
  });

  it("rejects non-ws websocket schemes", () => {
    expect(() =>
      parseRuntimeConfig(
        JSON.stringify({
          schemaVersion: 1,
          apiUrl: "https://api.example.com",
          wsUrl: "https://api.example.com/ws",
        }),
      ),
    ).toThrow(/wsUrl must use ws or wss/);
  });

  it("preserves electron-vite dev env precedence", () => {
    expect(
      runtimeConfigFromDevEnv({
        apiUrl: "http://dev-api.example.test:8080/",
        wsUrl: "ws://dev-api.example.test:8080/ws/",
        appUrl: "http://dev-app.example.test:3000/",
      }),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: "http://dev-api.example.test:8080",
      wsUrl: "ws://dev-api.example.test:8080/ws",
      appUrl: "http://dev-app.example.test:3000",
    });
  });
});
