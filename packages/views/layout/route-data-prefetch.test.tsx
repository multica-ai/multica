import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider, AppLink, type NavigationAdapter } from "../navigation";
import { RouteDataPrefetch } from "./route-data-prefetch";

function adapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
}

let qc: QueryClient;
let prefetched: unknown[][];

function renderLinks({ wsId }: { wsId: string | undefined } = { wsId: "ws-1" }) {
  return render(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter()}>
        <WorkspaceSlugProvider slug="acme">
          <RouteDataPrefetch wsId={wsId} userId="user-1">
            <AppLink href="/acme/agents">Agents</AppLink>
            <AppLink href="/acme/projects?view=board">Projects</AppLink>
            <AppLink href="/acme/settings">Settings</AppLink>
          </RouteDataPrefetch>
        </WorkspaceSlugProvider>
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.useFakeTimers();
  qc = new QueryClient();
  prefetched = [];
  vi.spyOn(qc, "prefetchQuery").mockImplementation(async (options) => {
    prefetched.push([...(options.queryKey as readonly unknown[])]);
  });
});

afterEach(() => {
  vi.useRealTimers();
});

const keys = () => prefetched.map((key) => JSON.stringify(key));

describe("RouteDataPrefetch", () => {
  it("warms the destination page's queries when a link is hovered", () => {
    renderLinks();
    fireEvent.mouseEnter(screen.getByText("Agents"));

    expect(prefetched.length).toBeGreaterThan(0);
    expect(prefetched.every((key) => key.includes("ws-1"))).toBe(true);
  });

  it("matches hrefs that carry a query string", () => {
    renderLinks();
    fireEvent.focus(screen.getByText("Projects"));

    expect(prefetched.length).toBeGreaterThan(0);
  });

  it("does nothing for routes without a registered page", () => {
    renderLinks();
    fireEvent.mouseEnter(screen.getByText("Settings"));

    expect(prefetched).toHaveLength(0);
  });

  it("warms the common list pages once the workspace is idle", () => {
    renderLinks();
    expect(prefetched).toHaveLength(0);

    vi.advanceTimersByTime(3000);

    const warmed = keys();
    expect(warmed.length).toBeGreaterThan(0);
    expect(new Set(warmed).size).toBeGreaterThanOrEqual(5);
  });

  it("stays idle without a resolved workspace", () => {
    renderLinks({ wsId: undefined });
    fireEvent.mouseEnter(screen.getByText("Agents"));
    vi.advanceTimersByTime(3000);

    expect(prefetched).toHaveLength(0);
  });
});
