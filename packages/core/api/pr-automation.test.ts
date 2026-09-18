import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { PRPolicyLinkSchema } from "./pr-automation";
const client = new ApiClient("https://api.example.test");
function reply(value: unknown) {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify(value), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
      ),
  );
}
afterEach(() => vi.unstubAllGlobals());
describe("PR automation API boundary", () => {
  it("does not turn a malformed response into an enabled policy", async () => {
    for (const bad of [
      null,
      {},
      {
        policy: { source: "all", auto_complete: "true", revision: 1 },
        migrated: true,
      },
    ]) {
      reply(bad);
      expect(await client.getPRPolicy("ws")).toBeNull();
      reply(bad);
      expect(await client.getIssuePRPolicy("issue")).toBeNull();
      reply(bad);
      await expect(
        client.previewPRPolicy("ws", {
          source: "all",
          autoComplete: true,
          revision: 1,
        }),
      ).rejects.toThrow();
    }
  });
  it("normalizes policy and sends only the selected policy with its preview token", async () => {
    const policy = { source: "title_branch", auto_complete: true, revision: 2 };
    reply({ policy, migrated: true });
    expect(await client.getPRPolicy("ws")).toEqual({
      policy: { source: "title_branch", autoComplete: true, revision: 2 },
      migrated: true,
    });
    reply({
      policy,
      migrated: true,
      issues: [],
      pending: [],
      token: "snapshot",
    });
    await client.updatePRPolicy(
      "ws",
      { source: "title_branch", autoComplete: true, revision: 2 },
      "snapshot",
    );
    expect(
      JSON.parse(vi.mocked(fetch).mock.calls[0]![1]!.body as string),
    ).toEqual({
      source: "title_branch",
      auto_complete: true,
      token: "snapshot",
    });
  });
  it("rejects executable PR URLs", () => {
    const link = {
      pr_id: "pr",
      provider: "github",
      title: "PR",
      url: "javascript:alert(1)",
      source: "manual",
      state: "merged",
      connected: true,
    };
    expect(PRPolicyLinkSchema.safeParse(link).success).toBe(false);
    expect(
      PRPolicyLinkSchema.parse({
        ...link,
        url: "https://github.com/a/b/pull/1",
      }).prId,
    ).toBe("pr");
  });
});
