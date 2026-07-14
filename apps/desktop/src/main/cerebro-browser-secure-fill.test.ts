import { describe, expect, it, vi } from "vitest";
import { performSecureFill } from "./cerebro-browser-secure-fill";

describe("performSecureFill", () => {
  it("injects the secret but never returns or audits it", async () => {
    const secret = "registry-password-must-not-leak";
    const fill = vi.fn(async () => undefined);
    const audit = vi.fn(async () => undefined);
    const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
    const fetchImpl: typeof fetch = async (input, init) => {
      requests.push({ input, init });
      return new Response(JSON.stringify({ value: secret }), { status: 200 });
    };
    const result = await performSecureFill({
      serverUrl: "https://multica.example",
      agentToken: "agent-token",
      desktopToken: "desktop-member-token",
      host: "registry.firtal.com",
      ref: "@e7",
      vault: "Shared/browser-login/registry",
      key: "PASSWORD",
      fetchImpl,
      fill,
      audit,
    });

    expect(fill).toHaveBeenCalledWith("@e7", secret);
    expect(requests[0]?.init?.headers).toMatchObject({
      authorization: "Bearer desktop-member-token",
    });
    expect(JSON.stringify(result)).not.toContain(secret);
    expect(JSON.stringify(audit.mock.calls)).not.toContain(secret);
  });
});
