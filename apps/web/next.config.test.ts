// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("dotenv", () => ({ config: vi.fn() }));
vi.mock("fumadocs-mdx/next", () => ({
  createMDX: () => (config: unknown) => config,
}));

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("allowedDevOrigins", () => {
  it.each([
    ["http://ph0x:13973", ["ph0x"]],
    [
      " https://dev.example.com:4443 , http://192.168.1.5:3001 ",
      ["dev.example.com", "192.168.1.5"],
    ],
    ["http://[::1]:13973", ["[::1]"]],
    ["http://localhost:3000,https://example.com", ["localhost", "example.com"]],
    ["*.example.com, ph0x", ["*.example.com", "ph0x"]],
    [" , ", undefined],
    [undefined, undefined],
  ])("normalizes %s to hostnames", async (origins, expected) => {
    vi.stubEnv("CORS_ALLOWED_ORIGINS", origins);
    const { default: config } = await import("./next.config");
    expect(config.allowedDevOrigins).toEqual(expected);
  });
});
