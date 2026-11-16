import { describe, expect, it } from "vitest";
import {
  OPENCLAW_GATEWAY_TOKEN_MASK,
  openclawRuntimeConfigEquals,
  parseOpenclawRuntimeConfig,
  serializeOpenclawRuntimeConfig,
} from "./openclaw-runtime-config";

describe("serializeOpenclawRuntimeConfig", () => {
  it("keeps the masked gateway token sentinel so the API can preserve the persisted token", () => {
    expect(
      serializeOpenclawRuntimeConfig({
        mode: "gateway",
        gateway: {
          host: "gw.internal",
          port: 18789,
          token: OPENCLAW_GATEWAY_TOKEN_MASK,
          tls: true,
        },
      }),
    ).toEqual({
      mode: "gateway",
      gateway: {
        host: "gw.internal",
        port: 18789,
        token: OPENCLAW_GATEWAY_TOKEN_MASK,
        tls: true,
      },
    });
  });

  it("omits an empty gateway token so users can clear a persisted token", () => {
    expect(
      serializeOpenclawRuntimeConfig({
        mode: "gateway",
        gateway: {
          host: "gw.internal",
          port: 18789,
        },
      }),
    ).toEqual({
      mode: "gateway",
      gateway: {
        host: "gw.internal",
        port: 18789,
      },
    });
  });

  it("passes through a real gateway token value", () => {
    expect(
      serializeOpenclawRuntimeConfig({
        mode: "gateway",
        gateway: {
          token: "rotated-secret",
        },
      }),
    ).toEqual({
      mode: "gateway",
      gateway: {
        token: "rotated-secret",
      },
    });
  });

  it("emits the agent model override alongside the routing config", () => {
    expect(
      serializeOpenclawRuntimeConfig({
        mode: "gateway",
        gateway: { host: "gw.internal", port: 18789 },
        modelOverride: "openrouter/@preset/agent-reasoning",
      }),
    ).toEqual({
      mode: "gateway",
      model_override: "openrouter/@preset/agent-reasoning",
      gateway: { host: "gw.internal", port: 18789 },
    });
  });

  it("omits an empty model override", () => {
    expect(serializeOpenclawRuntimeConfig({ mode: "local" })).toEqual({
      mode: "local",
    });
  });
});

describe("parseOpenclawRuntimeConfig", () => {
  it("reads and trims a persisted model override", () => {
    expect(
      parseOpenclawRuntimeConfig({
        mode: "local",
        model_override: "  openrouter/@preset/agent-reasoning  ",
      }),
    ).toEqual({ mode: "local", modelOverride: "openrouter/@preset/agent-reasoning" });
  });

  it("drops a non-string or blank model override", () => {
    expect(parseOpenclawRuntimeConfig({ model_override: 7 })).toEqual({});
    expect(parseOpenclawRuntimeConfig({ model_override: "   " })).toEqual({});
  });

  it("keeps the model override through a gateway round-trip so saving gateway settings cannot drop it", () => {
    const original = parseOpenclawRuntimeConfig({
      mode: "gateway",
      gateway: { host: "gw.internal", port: 18789 },
      model_override: "openrouter/@preset/agent-reasoning",
    });
    const resaved = parseOpenclawRuntimeConfig(
      serializeOpenclawRuntimeConfig(original),
    );
    expect(resaved.modelOverride).toBe("openrouter/@preset/agent-reasoning");
    expect(openclawRuntimeConfigEquals(original, resaved)).toBe(true);
  });
});

describe("openclawRuntimeConfigEquals", () => {
  it("detects a changed model override", () => {
    expect(
      openclawRuntimeConfigEquals(
        { modelOverride: "a" },
        { modelOverride: "b" },
      ),
    ).toBe(false);
    expect(
      openclawRuntimeConfigEquals(
        { modelOverride: "a" },
        { modelOverride: "a" },
      ),
    ).toBe(true);
  });
});
