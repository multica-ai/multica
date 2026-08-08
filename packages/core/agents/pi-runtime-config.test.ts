import { describe, expect, it } from "vitest";
import {
  parsePiRuntimeConfig,
  serializePiRuntimeConfig,
  validatePiRuntimeConfig,
} from "./pi-runtime-config";

describe("Pi runtime config", () => {
  it("round-trips only non-secret fields", () => {
    const parsed = parsePiRuntimeConfig({
      provider: "openai",
      api: "openai-responses",
      base_url: "https://api.example.com/v1",
      model: "gpt-5",
      api_key: "must-not-survive",
    });

    expect(serializePiRuntimeConfig(parsed)).toEqual({
      provider: "openai",
      api: "openai-responses",
      base_url: "https://api.example.com/v1",
      model: "gpt-5",
    });
  });

  it("requires a complete configuration", () => {
    expect(validatePiRuntimeConfig({ provider: "openai" })).toBe("incomplete");
    expect(validatePiRuntimeConfig({})).toBeNull();
  });

  it("requires HTTPS except for loopback development endpoints", () => {
    const base = {
      provider: "openai",
      api: "openai-responses" as const,
      model: "gpt-5",
    };
    expect(
      validatePiRuntimeConfig({ ...base, baseUrl: "http://api.example.com/v1" }),
    ).toBe("base_url");
    expect(
      validatePiRuntimeConfig({ ...base, baseUrl: "http://127.0.0.1:11434/v1" }),
    ).toBeNull();
    expect(
      validatePiRuntimeConfig({ ...base, baseUrl: "http://127.example.com/v1" }),
    ).toBe("base_url");
  });
});
