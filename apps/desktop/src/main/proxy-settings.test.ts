// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { changeProxySettings, createProxyEnvironment, maskProxyUrl, resolveProxyUrl } from "./proxy-settings";

describe("proxy environment", () => {
  it("merges bypass entries and restores all original upper/lower-case values after repeated updates", () => {
    const original = { HTTP_PROXY: "http://original:80", HTTPS_PROXY: "", NO_PROXY: "corp.local",
      http_proxy: "http://lower:80", no_proxy: "internal", OTHER: "keep" };
    const env: NodeJS.ProcessEnv = { ...original };
    const manager = createProxyEnvironment(env);
    manager.set("http://new:80", "extra.local");
    expect(env.HTTP_PROXY).toBe("http://new:80");
    expect(env.https_proxy).toBe("http://new:80");
    expect(env.NO_PROXY).toBe("localhost,127.0.0.1,::1,corp.local,internal,extra.local");
    manager.set("http://second:80", null);
    expect(env.NO_PROXY).not.toContain("extra.local");
    manager.set(null, null);
    expect(env).toEqual(original);
  });

  it("does not leave proxy variables behind when they were absent", () => {
    const env: NodeJS.ProcessEnv = {};
    const manager = createProxyEnvironment(env);
    manager.set("http://new:80", null);
    manager.set(null, null);
    expect(env).toEqual({});
  });
});

describe("proxy credential boundary", () => {
  it("redacts the password and preserves the real password when the masked placeholder is kept while editing", () => {
    const original = "http://user:secret@proxy.local:8080/";
    const masked = maskProxyUrl(original)!;
    expect(masked).not.toContain("secret");
    expect(resolveProxyUrl(masked, original)).toBe(original);
    expect(resolveProxyUrl("http://user:********@proxy.local:9090", original)).toBe("http://user:secret@proxy.local:9090");
    expect(resolveProxyUrl("http://other:8080", original)).toBe("http://other:8080");
    expect(resolveProxyUrl(null, original)).toBeNull();
    expect(() => resolveProxyUrl({}, original)).toThrow("Invalid proxy URL");
  });
});

describe("proxy changes", () => {
  const next = { proxyUrl: "http://user:secret@new:8080", noProxy: "corp" };
  function setup() {
    const order: string[] = [];
    return {
      order,
      dependencies: {
        current: { proxyUrl: null, noProxy: null },
        confirm: vi.fn(async () => { order.push("confirm"); return true; }),
        persist: vi.fn(async () => { order.push("persist"); return { ok: true }; }),
        apply: vi.fn(async () => { order.push("apply"); }),
        restart: vi.fn(async () => { order.push("restart"); return { success: true }; }),
      },
    };
  }

  it("confirms the host without credentials, then saves, applies and restarts", async () => {
    const { dependencies, order } = setup();
    expect(await changeProxySettings(next, dependencies)).toEqual({ ok: true });
    expect(dependencies.confirm).toHaveBeenCalledWith("new:8080");
    expect(order).toEqual(["confirm", "persist", "apply", "restart"]);
  });

  it("does not mutate or restart when the user cancels", async () => {
    const { dependencies } = setup();
    dependencies.confirm.mockResolvedValue(false);
    expect(await changeProxySettings(next, dependencies)).toEqual({ ok: false, cancelled: true });
    expect(dependencies.persist).not.toHaveBeenCalled();
    expect(dependencies.apply).not.toHaveBeenCalled();
    expect(dependencies.restart).not.toHaveBeenCalled();
  });

  it("does not restart unchanged settings", async () => {
    const { dependencies } = setup();
    await changeProxySettings(dependencies.current, dependencies);
    expect(dependencies.confirm).not.toHaveBeenCalled();
    expect(dependencies.restart).not.toHaveBeenCalled();
  });

  it("does not apply or restart after a failed write", async () => {
    const { dependencies } = setup();
    dependencies.persist.mockResolvedValue({ ok: false });
    expect((await changeProxySettings(next, dependencies)).ok).toBe(false);
    expect(dependencies.apply).not.toHaveBeenCalled();
    expect(dependencies.restart).not.toHaveBeenCalled();
  });

  it("restarts for clearing the proxy and for bypass-only changes", async () => {
    for (const updated of [{ proxyUrl: null, noProxy: null }, { ...next, noProxy: "new-bypass" }]) {
      const { dependencies } = setup();
      await changeProxySettings(updated, { ...dependencies, current: next });
      expect(dependencies.restart).toHaveBeenCalledOnce();
    }
  });

  it("reports a failed daemon restart instead of claiming full success", async () => {
    const { dependencies } = setup();
    dependencies.restart.mockResolvedValue({ success: false });
    const result = await changeProxySettings(next, dependencies);
    expect(result.ok).toBe(false);
    expect(result.error).toContain("could not restart");
  });
});