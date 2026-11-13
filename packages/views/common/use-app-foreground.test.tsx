import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useAppForeground } from "./use-app-foreground";

afterEach(() => { cleanup(); vi.restoreAllMocks(); delete (window as unknown as Record<string, unknown>).desktopAPI; });

it("uses native focus even when Electron's document still reports focus", async () => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  let report!: (foreground: boolean) => void;
  const unsubscribe = vi.fn();
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: {
    getWindowForeground: async () => false,
    onWindowForegroundChanged: (callback: typeof report) => { report = callback; return unsubscribe; },
  } });
  const view = renderHook(useAppForeground);
  await waitFor(() => expect(report).toBeTypeOf("function"));
  expect(view.result.current).toBe(false);
  act(() => report(true));
  expect(view.result.current).toBe(true);
  act(() => report(false));
  expect(view.result.current).toBe(false);
  view.unmount();
  expect(unsubscribe).toHaveBeenCalledOnce();
});

it("does not let an initial native query overwrite a newer blur event", async () => {
  let resolve!: (foreground: boolean) => void;
  let report!: (foreground: boolean) => void;
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: {
    getWindowForeground: () => new Promise<boolean>(r => { resolve = r; }),
    onWindowForegroundChanged: (callback: typeof report) => { report = callback; return () => {}; },
  } });
  const view = renderHook(useAppForeground);
  act(() => report(false));
  await act(async () => resolve(true));
  expect(view.result.current).toBe(false);
});

it("keeps browser foreground behavior based on DOM visibility and focus", () => {
  const focused = vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
  const view = renderHook(useAppForeground);
  expect(view.result.current).toBe(true);
  focused.mockReturnValue(false);
  act(() => window.dispatchEvent(new Event("blur")));
  expect(view.result.current).toBe(false);
});
