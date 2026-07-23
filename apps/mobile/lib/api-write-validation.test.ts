import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("expo-secure-store", () => ({
  getItemAsync: vi.fn(),
  setItemAsync: vi.fn(),
  deleteItemAsync: vi.fn(),
}));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("mobile ApiClient write response validation", () => {
  it("throws when updateIssue receives a malformed 2xx response", async () => {
    vi.stubEnv("EXPO_PUBLIC_API_URL", "https://api.example.test");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "issue-1" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const { api, ApiError } = await import("@/data/api");

    await expect(api.updateIssue("issue-1", { title: "New title" })).rejects.toBeInstanceOf(
      ApiError,
    );
  });

  it("throws when createProject receives a malformed 2xx response", async () => {
    vi.stubEnv("EXPO_PUBLIC_API_URL", "https://api.example.test");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "project-1" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const { api, ApiError } = await import("@/data/api");

    await expect(api.createProject({ title: "Mobile" })).rejects.toBeInstanceOf(
      ApiError,
    );
  });
});
