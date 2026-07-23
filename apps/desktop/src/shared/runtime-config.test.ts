import { describe, expect, it } from "vitest";
import {
  DEFAULT_RUNTIME_CONFIG,
  deriveWsUrl,
  parseDesktopConfigFile,
  parseRuntimeConfig,
  removeServerProfile,
  runtimeConfigFromDevEnv,
  serializeDesktopConfigFile,
  switchActiveServer,
  upsertServerProfile,
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

  it("strips the leading api. label when deriving appUrl", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({ schemaVersion: 1, apiUrl: "https://api.multica.ai" }),
      ),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: "https://api.multica.ai",
      wsUrl: "wss://api.multica.ai/ws",
      appUrl: "https://multica.ai",
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

  it("falls back to local web URL when dev apiUrl is localhost", () => {
    expect(runtimeConfigFromDevEnv({ apiUrl: "http://localhost:8080" })).toEqual({
      schemaVersion: 1,
      apiUrl: "http://localhost:8080",
      wsUrl: "ws://localhost:8080/ws",
      appUrl: "http://localhost:3000",
    });
  });

  it("derives dev appUrl by stripping the leading api. label", () => {
    // When the dev renderer is pointed at a remote backend (e.g. a test
    // environment), copy-link / share URLs must reflect that environment's
    // public web host, not the api host. Multica's convention exposes the
    // api at `api.<web-host>`, so stripping the leading label gives the
    // right web origin without a separate VITE_APP_URL.
    expect(
      runtimeConfigFromDevEnv({ apiUrl: "https://api.test.multica.ai" }),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: "https://api.test.multica.ai",
      wsUrl: "wss://api.test.multica.ai/ws",
      appUrl: "https://test.multica.ai",
    });
  });

  it("dev VITE_APP_URL still wins over apiUrl-derived value", () => {
    expect(
      runtimeConfigFromDevEnv({
        apiUrl: "https://api.test.multica.ai",
        appUrl: "https://staging.multica.ai",
      }),
    ).toEqual({
      schemaVersion: 1,
      apiUrl: "https://api.test.multica.ai",
      wsUrl: "wss://api.test.multica.ai/ws",
      appUrl: "https://staging.multica.ai",
    });
  });

  it("parses multi-server desktop.json with activeServerId", () => {
    const { config, servers } = parseDesktopConfigFile(
      JSON.stringify({
        schemaVersion: 1,
        activeServerId: "personal",
        servers: [
          { id: "cloud", name: "Multica Cloud", apiUrl: "https://api.multica.ai" },
          { id: "personal", name: "Personal", apiUrl: "http://127.0.0.1:28443" },
        ],
      }),
    );
    expect(config.apiUrl).toBe("http://127.0.0.1:28443");
    expect(servers.activeServerId).toBe("personal");
    expect(servers.servers.map((s) => s.id)).toEqual(["cloud", "personal"]);
  });

  it("round-trips multi-server state with legacy top-level fields", () => {
    const serialized = serializeDesktopConfigFile({
      editable: true,
      activeServerId: "company",
      servers: [
        {
          id: "cloud",
          name: "Multica Cloud",
          apiUrl: "https://api.multica.ai",
          wsUrl: "wss://api.multica.ai/ws",
          appUrl: "https://multica.ai",
        },
        {
          id: "company",
          name: "Company",
          apiUrl: "https://multica.corp.example",
          wsUrl: "wss://multica.corp.example/ws",
          appUrl: "https://multica.corp.example",
        },
      ],
    });
    const { config, servers } = parseDesktopConfigFile(serialized);
    expect(config.apiUrl).toBe("https://multica.corp.example");
    expect(servers.activeServerId).toBe("company");
    expect(servers.servers).toHaveLength(2);
  });

  it("upserts, switches, and removes server profiles", () => {
    let state = {
      editable: true,
      activeServerId: "cloud",
      servers: [
        {
          id: "cloud",
          name: "Multica Cloud",
          apiUrl: "https://api.multica.ai",
          wsUrl: "wss://api.multica.ai/ws",
          appUrl: "https://multica.ai",
        },
      ],
    };

    state = upsertServerProfile(state, {
      name: "Personal",
      apiUrl: "http://127.0.0.1:28443",
    });
    expect(state.servers).toHaveLength(2);

    state = switchActiveServer(state, state.servers[1]!.id);
    expect(state.activeServerId).toBe(state.servers[1]!.id);

    state = removeServerProfile(state, "cloud");
    expect(state.servers).toHaveLength(1);
    expect(state.activeServerId).toBe(state.servers[0]!.id);

    expect(() => removeServerProfile(state, state.servers[0]!.id)).toThrow(/last server/);
  });

  it("synthesizes a single server profile from legacy single-apiUrl files", () => {
    const { config, servers } = parseDesktopConfigFile(
      JSON.stringify({ schemaVersion: 1, apiUrl: "https://api.example.com" }),
    );
    expect(config.apiUrl).toBe("https://api.example.com");
    expect(servers.servers).toHaveLength(1);
    expect(servers.activeServerId).toBe(servers.servers[0]!.id);
    expect(servers.servers[0]!.name).toBe("api.example.com");
  });

  it("rejects unknown activeServerId", () => {
    expect(() =>
      parseDesktopConfigFile(
        JSON.stringify({
          schemaVersion: 1,
          activeServerId: "ghost",
          servers: [{ id: "cloud", name: "Cloud", apiUrl: "https://api.multica.ai" }],
        }),
      ),
    ).toThrow(/activeServerId/);
  });

  it("updates an existing profile by id without duplicating", () => {
    let state = {
      editable: true,
      activeServerId: "personal",
      servers: [
        {
          id: "personal",
          name: "Old",
          apiUrl: "http://127.0.0.1:28443",
          wsUrl: "ws://127.0.0.1:28443/ws",
          appUrl: "http://127.0.0.1:28443",
        },
      ],
    };
    state = upsertServerProfile(state, {
      id: "personal",
      name: "Personal Lab",
      apiUrl: "http://127.0.0.1:28443",
      appUrl: "http://app.local:3000",
    });
    expect(state.servers).toHaveLength(1);
    expect(state.servers[0]).toMatchObject({
      id: "personal",
      name: "Personal Lab",
      appUrl: "http://app.local:3000",
    });
  });

  it("rejects duplicate apiUrl on a different server id", () => {
    const state = {
      editable: true,
      activeServerId: "cloud",
      servers: [
        {
          id: "cloud",
          name: "Multica Cloud",
          apiUrl: "https://api.multica.ai",
          wsUrl: "wss://api.multica.ai/ws",
          appUrl: "https://multica.ai",
        },
      ],
    };
    expect(() =>
      upsertServerProfile(state, {
        name: "Also Cloud",
        apiUrl: "https://api.multica.ai/",
      }),
    ).toThrow(/already exists/);
  });

  it("rejects switching to an unknown server id", () => {
    const state = {
      editable: true,
      activeServerId: "cloud",
      servers: [
        {
          id: "cloud",
          name: "Multica Cloud",
          apiUrl: "https://api.multica.ai",
          wsUrl: "wss://api.multica.ai/ws",
          appUrl: "https://multica.ai",
        },
      ],
    };
    expect(() => switchActiveServer(state, "nope")).toThrow(/not found/);
  });
});

