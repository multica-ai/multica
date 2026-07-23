import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render } from "@testing-library/react";
import { ServiceWorkerRegister } from "./sw-register";

function setSecureContext(value: boolean) {
  Object.defineProperty(window, "isSecureContext", {
    value,
    configurable: true,
  });
}

function setServiceWorker(register: ReturnType<typeof vi.fn>) {
  Object.defineProperty(navigator, "serviceWorker", {
    value: { register },
    configurable: true,
  });
}

describe("ServiceWorkerRegister", () => {
  let warnSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    // jsdom reports document.readyState === "complete", so the effect registers
    // synchronously without needing to dispatch a `load` event.
    warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
    // Restore the jsdom default so tests relying on synchronous registration
    // are unaffected by the deferred-load case below.
    Object.defineProperty(document, "readyState", {
      value: "complete",
      configurable: true,
    });
  });

  it("registers /sw.js in production over a secure context", () => {
    vi.stubEnv("NODE_ENV", "production");
    setSecureContext(true);
    const register = vi.fn().mockResolvedValue({});
    setServiceWorker(register);

    render(<ServiceWorkerRegister />);

    expect(register).toHaveBeenCalledWith("/sw.js", { scope: "/" });
  });

  it("does not register outside production", () => {
    vi.stubEnv("NODE_ENV", "development");
    setSecureContext(true);
    const register = vi.fn();
    setServiceWorker(register);

    render(<ServiceWorkerRegister />);

    expect(register).not.toHaveBeenCalled();
  });

  it("skips registration and warns on a non-secure origin", () => {
    vi.stubEnv("NODE_ENV", "production");
    setSecureContext(false);
    const register = vi.fn();
    setServiceWorker(register);

    render(<ServiceWorkerRegister />);

    expect(register).not.toHaveBeenCalled();
    expect(warnSpy).toHaveBeenCalled();
  });

  it("defers registration to the load event while the page is still loading", () => {
    vi.stubEnv("NODE_ENV", "production");
    setSecureContext(true);
    Object.defineProperty(document, "readyState", {
      value: "loading",
      configurable: true,
    });
    const register = vi.fn().mockResolvedValue({});
    setServiceWorker(register);

    const { unmount } = render(<ServiceWorkerRegister />);
    // Nothing yet — registration is queued behind the window `load` event.
    expect(register).not.toHaveBeenCalled();

    window.dispatchEvent(new Event("load"));
    expect(register).toHaveBeenCalledWith("/sw.js", { scope: "/" });

    // Unmount removes the listener (cleanup branch); a second load is a no-op.
    unmount();
    window.dispatchEvent(new Event("load"));
    expect(register).toHaveBeenCalledTimes(1);
  });
});
