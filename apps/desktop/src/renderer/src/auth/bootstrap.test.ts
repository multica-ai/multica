import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { requestDesktopBootstrapToken } from "./bootstrap";

describe("requestDesktopBootstrapToken", () => {
  const originalFetch = global.fetch;

  beforeEach(() => {
    global.fetch = vi.fn();
  });

  afterEach(() => {
    global.fetch = originalFetch;
  });

  it("returns the bootstrap token on success", async () => {
    vi.mocked(global.fetch).mockResolvedValue(
      new Response(JSON.stringify({ token: "desktop-jwt" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(
      requestDesktopBootstrapToken("http://localhost:8080"),
    ).resolves.toEqual({ kind: "success", token: "desktop-jwt" });
    expect(global.fetch).toHaveBeenCalledWith(
      "http://localhost:8080/auth/bootstrap/token",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("treats missing bootstrap support as a compatibility fallback", async () => {
    vi.mocked(global.fetch).mockResolvedValue(
      new Response(null, { status: 409, statusText: "Conflict" }),
    );

    await expect(
      requestDesktopBootstrapToken("http://localhost:8080/"),
    ).resolves.toEqual({ kind: "unsupported", status: 409 });
  });

  it("surfaces network failures as recoverable errors", async () => {
    vi.mocked(global.fetch).mockRejectedValue(new Error("connect ECONNREFUSED"));

    await expect(
      requestDesktopBootstrapToken("http://localhost:8080"),
    ).resolves.toEqual({
      kind: "error",
      message: "connect ECONNREFUSED",
    });
  });

  it("rejects successful responses that omit the token", async () => {
    vi.mocked(global.fetch).mockResolvedValue(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(
      requestDesktopBootstrapToken("http://localhost:8080"),
    ).resolves.toEqual({
      kind: "error",
      status: 200,
      message: "Desktop bootstrap response did not include a token",
    });
  });
});
