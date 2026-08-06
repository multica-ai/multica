import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The module memoises the registration, so each test re-imports a fresh copy.
async function importFresh() {
  vi.resetModules();
  return import("./service-worker");
}

interface FakeServiceWorkerContainer {
  controller: ServiceWorker | null;
  register: ReturnType<typeof vi.fn>;
  getRegistrations: ReturnType<typeof vi.fn>;
  addEventListener: (type: string, listener: () => void) => void;
  dispatch: (type: string) => void;
}

function installFakeContainer(
  registrations: ServiceWorkerRegistration[] = [],
): FakeServiceWorkerContainer {
  const listeners = new Map<string, Array<() => void>>();
  const container: FakeServiceWorkerContainer = {
    controller: null,
    register: vi.fn(() => Promise.resolve({} as ServiceWorkerRegistration)),
    getRegistrations: vi.fn(() => Promise.resolve(registrations)),
    addEventListener: (type, listener) => {
      listeners.set(type, [...(listeners.get(type) ?? []), listener]);
    },
    dispatch: (type) => {
      for (const listener of listeners.get(type) ?? []) listener();
    },
  };
  Object.defineProperty(navigator, "serviceWorker", {
    value: container,
    configurable: true,
  });
  return container;
}

describe("registerServiceWorker", () => {
  beforeEach(() => {
    vi.stubEnv("NODE_ENV", "production");
    Reflect.deleteProperty(navigator, "serviceWorker");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("returns null when service workers are unsupported", async () => {
    const { registerServiceWorker } = await importFresh();
    expect(registerServiceWorker()).toBeNull();
  });

  it("registers /sw.js once and returns the same registration", async () => {
    const container = installFakeContainer();
    const { registerServiceWorker } = await importFresh();

    const first = registerServiceWorker();
    const second = registerServiceWorker();

    expect(container.register).toHaveBeenCalledTimes(1);
    expect(container.register).toHaveBeenCalledWith("/sw.js");
    expect(second).toBe(first);
    await expect(first).resolves.toEqual({});
  });

  it("removes existing registrations and caches outside production", async () => {
    vi.stubEnv("NODE_ENV", "development");
    const unregister = vi.fn(() => Promise.resolve(true));
    const container = installFakeContainer([
      { unregister } as unknown as ServiceWorkerRegistration,
    ]);
    const cacheStorage = {
      keys: vi.fn(() => Promise.resolve(["multica-offline-v1", "old-cache"])),
      delete: vi.fn(() => Promise.resolve(true)),
    };
    vi.stubGlobal("caches", cacheStorage);
    const { registerServiceWorker } = await importFresh();

    await expect(registerServiceWorker()).resolves.toBeNull();

    expect(container.register).not.toHaveBeenCalled();
    expect(container.getRegistrations).toHaveBeenCalledTimes(1);
    expect(unregister).toHaveBeenCalledTimes(1);
    expect(cacheStorage.delete).toHaveBeenCalledTimes(2);
    expect(cacheStorage.delete).toHaveBeenCalledWith("multica-offline-v1");
    expect(cacheStorage.delete).toHaveBeenCalledWith("old-cache");
  });

  it("does not reload on the first controller claim", async () => {
    const container = installFakeContainer();
    const reload = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      reload,
    });
    const { registerServiceWorker } = await importFresh();

    registerServiceWorker();
    container.dispatch("controllerchange");

    expect(reload).not.toHaveBeenCalled();
  });

  it("reloads when an already-controlled page gets a new controller", async () => {
    const container = installFakeContainer();
    container.controller = {} as ServiceWorker;
    const reload = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      reload,
    });
    const { registerServiceWorker } = await importFresh();

    registerServiceWorker();
    container.dispatch("controllerchange");

    expect(reload).toHaveBeenCalledTimes(1);
  });
});
