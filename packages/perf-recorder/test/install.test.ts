import { afterEach, describe, expect, it, vi } from "vitest";
import {
  installRecorderHook,
  isRecorderHookInstalled,
  subscribeCommit,
  uninstallRecorderHook,
} from "../src/install";

const HOOK_KEY = "__REACT_DEVTOOLS_GLOBAL_HOOK__";

function getHook(): any {
  return (window as unknown as Record<string, unknown>)[HOOK_KEY];
}

afterEach(() => {
  uninstallRecorderHook();
});

describe("installRecorderHook (Desktop hook-order hard gate)", () => {
  it("installs the DevTools global hook synchronously", () => {
    installRecorderHook();
    expect(isRecorderHookInstalled()).toBe(true);
    const hook = getHook();
    expect(hook).toBeDefined();
    expect(typeof hook.onCommitFiberRoot).toBe("function");
  });

  it("captures a commit from a renderer that registers AFTER the hook is installed", () => {
    // This is the ordering guarantee in miniature: the hook is installed first,
    // then a React-like renderer injects and commits, and we still receive it.
    installRecorderHook();
    const listener = vi.fn();
    subscribeCommit(listener);

    const hook = getHook();
    // Simulate react-dom registering itself, then committing a root.
    if (typeof hook.inject === "function") {
      hook.inject({ version: "19.0.0", rendererPackageName: "react-dom", findFiberByHostInstance: () => null });
    }
    const root = { current: { actualDuration: 12, alternate: null, child: null, sibling: null } };
    hook.onCommitFiberRoot(1, root);

    expect(listener).toHaveBeenCalledTimes(1);
    expect(listener.mock.calls[0]?.[0]).toBe(root);
  });

  it("is idempotent — a second install does not double-dispatch commits", () => {
    installRecorderHook();
    installRecorderHook();
    const listener = vi.fn();
    subscribeCommit(listener);
    const hook = getHook();
    if (typeof hook.inject === "function") hook.inject({ version: "19.0.0", rendererPackageName: "react-dom" });
    hook.onCommitFiberRoot(1, { current: { actualDuration: 1, alternate: null } });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("stops dispatching after uninstall", () => {
    installRecorderHook();
    const listener = vi.fn();
    subscribeCommit(listener);
    uninstallRecorderHook();
    expect(isRecorderHookInstalled()).toBe(false);
    listener.mockClear();
    // Any further calls must not reach our (now cleared) listeners.
    const hook = getHook();
    if (hook && typeof hook.onCommitFiberRoot === "function") {
      hook.onCommitFiberRoot(1, { current: { actualDuration: 1, alternate: null } });
    }
    expect(listener).not.toHaveBeenCalled();
  });
});
