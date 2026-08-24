import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

// lib/queries.ts and lib/agentfarm-api.ts are both `server-only` and touch
// real infra (Postgres / the Go API) — mock them at the module boundary so
// this test exercises only the route handler's own LBYL branching, same
// spirit as lib/agentfarm-api.test.ts stubbing `server-only` itself.
vi.mock("@/lib/queries", () => ({
  getWorkspaceMetadata: vi.fn(),
  getWorkspaceMembers: vi.fn(),
  getPendingInvitations: vi.fn(),
}));
vi.mock("@/lib/agentfarm-api", () => ({
  createWorkspaceInvitation: vi.fn(),
}));

const { getWorkspaceMetadata, getWorkspaceMembers, getPendingInvitations } = await import("@/lib/queries");
const { createWorkspaceInvitation } = await import("@/lib/agentfarm-api");
const { POST } = await import("./route");

function postRequest(body: unknown): NextRequest {
  return new NextRequest("http://localhost/api/workspaces/ws-1/invitations", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

const ELIGIBLE_BOT_MEMBER = { id: "bot-1", name: "Bot", email: "agentfarm-bot@g2.com", role: "owner" as const };

describe("POST /api/workspaces/[id]/invitations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubEnv("BOT_PAT", "test-pat");
    vi.mocked(getWorkspaceMetadata).mockResolvedValue({
      id: "ws-1",
      slug: "acme",
      createdAt: "2026-01-01T00:00:00Z",
      owner: null,
      model: null,
      root: null,
      repoCount: 0,
    });
    vi.mocked(getWorkspaceMembers).mockResolvedValue([ELIGIBLE_BOT_MEMBER]);
    vi.mocked(getPendingInvitations).mockResolvedValue([]);
  });

  it("rejects an invalid body before touching any data source", async () => {
    const res = await POST(postRequest({ email: "not-an-email", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(res.status).toBe(400);
    expect(getWorkspaceMetadata).not.toHaveBeenCalled();
  });

  it("404s when the workspace doesn't exist", async () => {
    vi.mocked(getWorkspaceMetadata).mockResolvedValue(null);
    const res = await POST(postRequest({ email: "new@example.com", role: "member" }), {
      params: Promise.resolve({ id: "missing" }),
    });
    expect(res.status).toBe(404);
    expect(createWorkspaceInvitation).not.toHaveBeenCalled();
  });

  it("409s when the bot isn't owner/admin, without calling the Go API", async () => {
    vi.mocked(getWorkspaceMembers).mockResolvedValue([{ ...ELIGIBLE_BOT_MEMBER, role: "member" }]);
    const res = await POST(postRequest({ email: "new@example.com", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.error).toMatch(/isn't an owner\/admin/i);
    expect(createWorkspaceInvitation).not.toHaveBeenCalled();
  });

  it("409s with a distinct message when BOT_PAT is unset, without calling the Go API", async () => {
    vi.stubEnv("BOT_PAT", "");
    const res = await POST(postRequest({ email: "new@example.com", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.error).toMatch(/bot_pat.*isn't configured/i);
    expect(createWorkspaceInvitation).not.toHaveBeenCalled();
  });

  it("409s when the email is already a member, without calling the Go API", async () => {
    vi.mocked(getWorkspaceMembers).mockResolvedValue([
      ELIGIBLE_BOT_MEMBER,
      { id: "user-2", name: "Existing", email: "existing@example.com", role: "member" },
    ]);
    const res = await POST(postRequest({ email: "existing@example.com", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.error).toMatch(/already a member/i);
    expect(createWorkspaceInvitation).not.toHaveBeenCalled();
  });

  it("409s when there's already a pending invitation for the email, without calling the Go API", async () => {
    vi.mocked(getPendingInvitations).mockResolvedValue([
      { email: "invited@example.com", role: "member", createdAt: "2026-01-01T00:00:00Z", expiresAt: "2026-01-08T00:00:00Z" },
    ]);
    const res = await POST(postRequest({ email: "invited@example.com", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.error).toMatch(/pending invitation/i);
    expect(createWorkspaceInvitation).not.toHaveBeenCalled();
  });

  it("calls the Go API only after every pre-check passes, and returns 201 on success", async () => {
    vi.mocked(createWorkspaceInvitation).mockResolvedValue({
      ok: true,
      invitation: {
        id: "inv-1",
        workspace_id: "ws-1",
        invitee_email: "new@example.com",
        role: "member",
        status: "pending",
        created_at: "2026-01-01T00:00:00Z",
        expires_at: "2026-01-08T00:00:00Z",
      },
    });
    const res = await POST(postRequest({ email: "new@example.com", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(createWorkspaceInvitation).toHaveBeenCalledWith("ws-1", "new@example.com", "member");
    expect(res.status).toBe(201);
  });

  it("maps a same-instant race from the Go API through as a last-resort fallback", async () => {
    vi.mocked(createWorkspaceInvitation).mockResolvedValue({
      ok: false,
      status: 409,
      message: "invitation already pending for this email",
    });
    const res = await POST(postRequest({ email: "new@example.com", role: "member" }), {
      params: Promise.resolve({ id: "ws-1" }),
    });
    expect(res.status).toBe(409);
    const body = await res.json();
    expect(body.error).toBe("invitation already pending for this email");
  });
});
