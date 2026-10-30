import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "./context";
import { useBackOrReplace } from "./use-back-or-replace";
import type { NavigationAdapter } from "./types";

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function Probe() {
  const backOrReplace = useBackOrReplace();
  return (
    <button onClick={() => backOrReplace("/acme/issues")}>leave</button>
  );
}

function renderProbe(adapter: NavigationAdapter) {
  render(
    <NavigationProvider value={adapter}>
      <Probe />
    </NavigationProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "leave" }));
}

describe("useBackOrReplace", () => {
  it("goes back when the platform reports in-app history", () => {
    const adapter = makeAdapter({ canGoBack: () => true });

    renderProbe(adapter);

    expect(adapter.back).toHaveBeenCalledTimes(1);
    expect(adapter.replace).not.toHaveBeenCalled();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("replaces with the fallback when there is nothing to go back to", () => {
    const adapter = makeAdapter({ canGoBack: () => false });

    renderProbe(adapter);

    expect(adapter.replace).toHaveBeenCalledWith("/acme/issues");
    expect(adapter.back).not.toHaveBeenCalled();
  });

  it("replaces with the fallback when the adapter cannot report history", () => {
    const adapter = makeAdapter();

    renderProbe(adapter);

    expect(adapter.replace).toHaveBeenCalledWith("/acme/issues");
    expect(adapter.back).not.toHaveBeenCalled();
  });

  it("never pushes — the page being left is dead and must not stay in history", () => {
    const adapter = makeAdapter({ canGoBack: () => false });

    renderProbe(adapter);

    expect(adapter.push).not.toHaveBeenCalled();
  });
});
