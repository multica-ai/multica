import { describe, expect, it } from "vitest";

import {
  deriveWsUrl,
  resolveApiBaseUrl,
  resolveBasePath,
  withBasePath,
} from "./base-path";

describe("base path helpers", () => {
  it("normalizes empty, root, and slashless base paths", () => {
    expect(resolveBasePath({})).toBe("");
    expect(resolveBasePath({ NEXT_PUBLIC_BASE_PATH: "" })).toBe("");
    expect(resolveBasePath({ NEXT_PUBLIC_BASE_PATH: "/" })).toBe("");
    expect(resolveBasePath({ NEXT_PUBLIC_BASE_PATH: "multica/" })).toBe(
      "/multica",
    );
  });

  it("uses BASE_PATH as a server-side alias", () => {
    expect(resolveBasePath({ BASE_PATH: "/ops/multica/" })).toBe("/ops/multica");
  });

  it("prefixes same-origin paths without double-prefixing", () => {
    expect(withBasePath("/multica", "/api/config")).toBe("/multica/api/config");
    expect(withBasePath("/multica", "/multica/api/config")).toBe(
      "/multica/api/config",
    );
    expect(withBasePath("", "/api/config")).toBe("/api/config");
  });

  it("keeps explicit API and WS URLs ahead of the base path", () => {
    expect(
      resolveApiBaseUrl({
        NEXT_PUBLIC_API_URL: "https://api.example.test",
        NEXT_PUBLIC_BASE_PATH: "/multica",
      }),
    ).toBe("https://api.example.test");
    expect(
      deriveWsUrl(
        {
          NEXT_PUBLIC_WS_URL: "wss://api.example.test/ws",
          NEXT_PUBLIC_BASE_PATH: "/multica",
        },
        { protocol: "https:", host: "example.test" },
      ),
    ).toBe("wss://api.example.test/ws");
  });

  it("derives same-origin API and WS URLs under the base path", () => {
    const env = { NEXT_PUBLIC_BASE_PATH: "/multica" };
    expect(resolveApiBaseUrl(env)).toBe("/multica");
    expect(deriveWsUrl(env, { protocol: "https:", host: "example.test" })).toBe(
      "wss://example.test/multica/ws",
    );
    expect(deriveWsUrl(env, { protocol: "http:", host: "localhost:3000" })).toBe(
      "ws://localhost:3000/multica/ws",
    );
  });
});
