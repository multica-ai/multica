import { afterEach, describe, expect, it, vi } from "vitest";

// `server-only` unconditionally throws outside Next's own webpack build (it
// relies on a bundler alias that vitest doesn't apply) — stub it so this
// server-only-tagged module can be imported directly in a test, same trick
// used for testing any other `server-only` module in isolation.
vi.mock("server-only", () => ({}));

const { createWorkspaceInvitation } = await import("./agentfarm-api");

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("createWorkspaceInvitation", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it("returns ok:true with the parsed invitation on 201", async () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    const invitation = {
      id: "inv-1",
      workspace_id: "ws-1",
      invitee_email: "new@example.com",
      role: "member",
      status: "pending",
      created_at: "2026-01-01T00:00:00Z",
      expires_at: "2026-01-08T00:00:00Z",
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(201, invitation));
    vi.stubGlobal("fetch", fetchMock);

    const result = await createWorkspaceInvitation("ws-1", "new@example.com", "member");

    expect(result).toEqual({ ok: true, invitation });
    expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost:8080/api/workspaces/ws-1/members",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ Authorization: "Bearer test-pat" }),
      }),
    );
  });

  it("fails fast without calling fetch when BOT_PAT is not configured", async () => {
    vi.stubEnv("BOT_PAT", "");
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const result = await createWorkspaceInvitation("ws-1", "new@example.com", "member");

    expect(result).toEqual({ ok: false, status: 500, message: "BOT_PAT is not configured" });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("returns ok:false with the Go API's error message on a 409", async () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(409, { error: "user is already a member" })));

    const result = await createWorkspaceInvitation("ws-1", "existing@example.com", "member");

    expect(result).toEqual({ ok: false, status: 409, message: "user is already a member" });
  });

  it("falls back to a generic message when a non-ok response body doesn't match ApiErrorSchema", async () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(500, { unexpected: "shape" })));

    const result = await createWorkspaceInvitation("ws-1", "someone@example.com", "member");

    expect(result).toEqual({ ok: false, status: 500, message: "Go API request failed: 500" });
  });

  it("treats a malformed 2xx response as a failure rather than casting it (parse, don't cast)", async () => {
    vi.stubEnv("BOT_PAT", "test-pat");
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(201, { unexpected: "shape" })));

    const result = await createWorkspaceInvitation("ws-1", "someone@example.com", "member");

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toMatch(/malformed/i);
      // Not the original 201: callers (route handler, then hooks.ts's
      // fetchJson) branch on the HTTP status to decide success/failure, so a
      // 2xx here would make this failure read as a success downstream.
      expect(result.status).toBe(502);
    }
    warnSpy.mockRestore();
  });
});
