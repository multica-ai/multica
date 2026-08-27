// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { syncStaticAssetServiceWorker } from "./register-static-sw";

function container() {
  return {
    register: vi.fn().mockResolvedValue(undefined),
    getRegistrations: vi.fn().mockResolvedValue([]),
  };
}

describe("syncStaticAssetServiceWorker", () => {
  it("does nothing when the browser has no service worker", () => {
    expect(
      syncStaticAssetServiceWorker({ production: true }),
    ).toBeTypeOf("function");
  });

  it("registers after load so first paint is not competing with Cache Storage", () => {
    const serviceWorker = container();
    const cleanup = vi.fn();
    const onLoad = vi.fn((handler: () => void) => {
      handler();
      return cleanup;
    });

    const stop = syncStaticAssetServiceWorker({
      production: true,
      serviceWorker,
      documentReadyState: "loading",
      onLoad,
    });

    expect(onLoad).toHaveBeenCalledOnce();
    expect(serviceWorker.register).toHaveBeenCalledWith("/sw.js", {
      scope: "/",
      updateViaCache: "none",
    });
    stop();
    expect(cleanup).toHaveBeenCalledOnce();
  });

  it("registers immediately when the page has already loaded", () => {
    const serviceWorker = container();

    syncStaticAssetServiceWorker({
      production: true,
      serviceWorker,
      documentReadyState: "complete",
    });

    expect(serviceWorker.register).toHaveBeenCalledWith("/sw.js", {
      scope: "/",
      updateViaCache: "none",
    });
  });

  it("unregisters a leftover worker outside production", async () => {
    const ours = { unregister: vi.fn(), active: { scriptURL: "https://localhost/sw.js" } };
    const other = {
      unregister: vi.fn(),
      active: { scriptURL: "https://localhost/other-sw.js" },
    };
    const serviceWorker = {
      register: vi.fn(),
      getRegistrations: vi.fn().mockResolvedValue([ours, other]),
    };

    syncStaticAssetServiceWorker({
      production: false,
      serviceWorker,
    });

    await vi.waitFor(() => {
      expect(ours.unregister).toHaveBeenCalledOnce();
    });
    expect(other.unregister).not.toHaveBeenCalled();
    expect(serviceWorker.register).not.toHaveBeenCalled();
  });
});
