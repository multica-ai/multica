import { describe, expect, it } from "vitest";
import {
  resolveEnvironmentHint,
  stripEnvironmentTitleSuffix,
  withEnvironmentTitleSuffix,
} from "./environment-hint";
import type { RuntimeConfigResult } from "./runtime-config";

function okResult(
  apiUrl: string,
  name: string,
  extras?: Partial<RuntimeConfigResult & { ok: true }>,
): RuntimeConfigResult {
  return {
    ok: true,
    config: {
      schemaVersion: 1,
      apiUrl,
      wsUrl: `${apiUrl.replace(/^http/, "ws")}/ws`,
      appUrl: apiUrl,
    },
    servers: {
      editable: true,
      activeServerId: "active",
      servers: [
        {
          id: "active",
          name,
          apiUrl,
          wsUrl: `${apiUrl.replace(/^http/, "ws")}/ws`,
          appUrl: apiUrl,
        },
      ],
    },
    ...extras,
  };
}

describe("resolveEnvironmentHint", () => {
  it("returns null when runtime config failed to load", () => {
    expect(
      resolveEnvironmentHint({
        ok: false,
        error: { message: "bad config" },
      }),
    ).toBeNull();
  });

  it("hides Multica Cloud (including trailing-slash variants)", () => {
    expect(
      resolveEnvironmentHint(okResult("https://api.multica.ai", "Multica Cloud")),
    ).toBeNull();
    expect(
      resolveEnvironmentHint(okResult("https://api.multica.ai/", "Cloud")),
    ).toBeNull();
  });

  it("shows self-hosted and company backends", () => {
    expect(
      resolveEnvironmentHint(okResult("http://127.0.0.1:28443", "Personal")),
    ).toEqual({ name: "Personal", apiUrl: "http://127.0.0.1:28443" });
    expect(
      resolveEnvironmentHint(
        okResult("https://multica.corp.example", "Company"),
      ),
    ).toEqual({
      name: "Company",
      apiUrl: "https://multica.corp.example",
    });
  });

  it("uses the active server even when multiple are configured", () => {
    const result: RuntimeConfigResult = {
      ok: true,
      config: {
        schemaVersion: 1,
        apiUrl: "http://127.0.0.1:28443",
        wsUrl: "ws://127.0.0.1:28443/ws",
        appUrl: "http://127.0.0.1:28443",
      },
      servers: {
        editable: true,
        activeServerId: "personal",
        servers: [
          {
            id: "cloud",
            name: "Multica Cloud",
            apiUrl: "https://api.multica.ai",
            wsUrl: "wss://api.multica.ai/ws",
            appUrl: "https://multica.ai",
          },
          {
            id: "personal",
            name: "Personal",
            apiUrl: "http://127.0.0.1:28443",
            wsUrl: "ws://127.0.0.1:28443/ws",
            appUrl: "http://127.0.0.1:28443",
          },
        ],
      },
    };
    expect(resolveEnvironmentHint(result)).toEqual({
      name: "Personal",
      apiUrl: "http://127.0.0.1:28443",
    });
  });

  it("returns null when activeServerId is missing from the list", () => {
    const result = okResult("http://127.0.0.1:1", "X");
    if (!result.ok) return;
    result.servers.activeServerId = "missing";
    expect(resolveEnvironmentHint(result)).toBeNull();
  });
});

describe("window title suffix", () => {
  it("appends and strips without doubling", () => {
    expect(withEnvironmentTitleSuffix("Issues", "Personal")).toBe(
      "Issues · Personal",
    );
    expect(withEnvironmentTitleSuffix("Issues · Personal", "Personal")).toBe(
      "Issues · Personal",
    );
    expect(stripEnvironmentTitleSuffix("Issues · Personal", "Personal")).toBe(
      "Issues",
    );
  });

  it("falls back to Multica when the base title is empty", () => {
    expect(withEnvironmentTitleSuffix("", "Personal")).toBe("Multica · Personal");
    expect(withEnvironmentTitleSuffix("   ", "Personal")).toBe(
      "Multica · Personal",
    );
  });
});
