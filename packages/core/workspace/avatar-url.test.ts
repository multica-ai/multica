import { describe, expect, it, vi } from "vitest";
import { resolvePublicFileUrl, resolvePublicFileUrlWithBase } from "./avatar-url";

vi.mock("../api", () => ({
  api: {
    getBaseUrl: () => "http://127.0.0.1:8080",
  },
}));

describe("resolvePublicFileUrlWithBase", () => {
  it("resolves root-relative URLs against base URL", () => {
    expect(resolvePublicFileUrlWithBase("/uploads/a.png", "http://127.0.0.1:8080")).toBe(
      "http://127.0.0.1:8080/uploads/a.png",
    );
  });

  it("trims trailing slash in base URL", () => {
    expect(resolvePublicFileUrlWithBase("/upload/a.png", "http://127.0.0.1:8080/")).toBe(
      "http://127.0.0.1:8080/upload/a.png",
    );
  });

  it("keeps absolute URLs unchanged", () => {
    expect(resolvePublicFileUrlWithBase("https://cdn.example.com/a.png", "http://127.0.0.1:8080")).toBe(
      "https://cdn.example.com/a.png",
    );
  });

  it("returns null for empty values", () => {
    expect(resolvePublicFileUrlWithBase(null, "http://127.0.0.1:8080")).toBeNull();
    expect(resolvePublicFileUrlWithBase(undefined, "http://127.0.0.1:8080")).toBeNull();
  });
});

describe("resolvePublicFileUrl", () => {
  it("uses API base URL implicitly", () => {
    expect(resolvePublicFileUrl("/uploads/a.png")).toBe("http://127.0.0.1:8080/uploads/a.png");
  });
});
