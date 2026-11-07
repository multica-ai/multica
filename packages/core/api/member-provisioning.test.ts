// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { EMPTY_PROVISION_MEMBERS_RESPONSE, ProvisionMembersResponseSchema } from "./schemas";

afterEach(() => vi.unstubAllGlobals());

describe("member provisioning response boundary", () => {
  it("supports added outcomes and defaults counts from an older provisioning server", () => {
    const olderSummary = { total: 1, created: 0, already_member: 0, duplicate: 0, invalid: 0, failed: 0 };
    const result = { email: "seed@example.test", role: "admin", status: "added" };
    expect(ProvisionMembersResponseSchema.parse({ summary: olderSummary, results: [result] }).summary.added).toBe(0);
    expect(ProvisionMembersResponseSchema.parse({ summary: olderSummary, results: [result] }).results[0]?.status).toBe("added");
    expect(ProvisionMembersResponseSchema.parse({ summary: olderSummary, results: [{ ...result, role: "owner", status: "already_member" }] }).results[0]?.role).toBe("owner");
    expect(ProvisionMembersResponseSchema.safeParse({ summary: olderSummary, results: [{ ...result, status: "future" }] }).success).toBe(false);
  });
  const response = {
    summary: { total: 1, created: 1, added: 0, already_member: 0, duplicate: 0, invalid: 0, failed: 0 },
    results: [{ email: "seed@example.test", role: "member", status: "created", future: true }],
  };

  it("posts the cohort to the workspace endpoint and preserves future fields", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(response)));
    vi.stubGlobal("fetch", fetchMock);
    const data = { entries: [{ email: "seed@example.test", name: "Display Name", role: "member" as const }] };
    await expect(new ApiClient("https://example.test").provisionMembers("workspace-1", data)).resolves.toEqual(response);
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://example.test/api/workspaces/workspace-1/members/provision");
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ method: "POST", body: JSON.stringify(data) }));
  });

  it("falls back safely for malformed successful responses", async () => {
    for (const raw of [{ summary: {}, results: "ok" }, { ...response, results: [{ email: "seed@example.test", role: "owner", status: "created" }] }]) {
      expect(ProvisionMembersResponseSchema.safeParse(raw).success).toBe(false);
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(raw))));
      await expect(new ApiClient("https://example.test").provisionMembers("workspace-1", { entries: [] })).resolves.toEqual(EMPTY_PROVISION_MEMBERS_RESPONSE);
    }
  });
});
