import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { issueKeys } from "@multica/core/issues/queries";
import type { Issue } from "@multica/core/types";
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
let fetched: unknown[][];

const ISSUE_ID = "0b7f3c2e-5d1a-4c8e-9f6b-2a4d8e1c3b5f";
const OTHER_ID = "7e2a9d4c-1b3f-4a6e-8c5d-9f0b2e4a6c8d";

function renderLinks({ wsId, guest = false }: { wsId: string | undefined; guest?: boolean } = { wsId: "ws-1" }) {
  return render(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter()}>
        <WorkspaceSlugProvider slug="acme">
          <RouteDataPrefetch wsId={wsId} userId="user-1" guest={guest}>
            <AppLink href="/acme/agents">Agents</AppLink>
            <AppLink href="/acme/projects?view=board">Projects</AppLink>
            <AppLink href="/acme/settings">Settings</AppLink>
            <AppLink href="/acme/issues/MUL-7">Issue by identifier</AppLink>
            <AppLink href={`/acme/issues/${ISSUE_ID}`}>Issue by id</AppLink>
            <AppLink href={`/acme/issues/${OTHER_ID}`}>Other issue</AppLink>
            <AppLink href={`/acme/issues/${ISSUE_ID}/deliverables/report.html`}>Deliverable</AppLink>
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
  fetched = [];
  vi.spyOn(qc, "fetchQuery").mockImplementation(async (options) => {
    const key = options.queryKey as readonly unknown[];
    fetched.push([...key]);
    const ref = key.at(-1);
    return { id: ref === "MUL-7" ? ISSUE_ID : ref, identifier: "MUL-7" } as Issue;
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
    // The assignee picker ranks from local picks; nothing warms server counts.
    expect(warmed.some((key) => key.includes("assignee-frequency"))).toBe(false);
  });

  it("stays idle without a resolved workspace", () => {
    renderLinks({ wsId: undefined });
    fireEvent.mouseEnter(screen.getByText("Agents"));
    vi.advanceTimersByTime(3000);

    expect(prefetched).toHaveLength(0);
  });

  it("fetches a hovered issue once the pointer rests, and what its page fills in after", async () => {
    renderLinks();
    fireEvent.mouseEnter(screen.getByText("Issue by identifier"));
    await vi.advanceTimersByTimeAsync(39);
    expect(fetched).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(1);
    expect(fetched).toEqual([[...issueKeys.detail("ws-1", "MUL-7")]]);
    // The page resolves the identifier, then keys everything else by UUID.
    expect(qc.getQueryData(issueKeys.detail("ws-1", ISSUE_ID))).toMatchObject({ id: ISSUE_ID });
    expect(prefetched).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(40);
    expect(keys()).toEqual(
      expect.arrayContaining([
        JSON.stringify(issueKeys.timeline(ISSUE_ID)),
        JSON.stringify(issueKeys.children("ws-1", ISSUE_ID)),
      ]),
    );
    expect(keys()).not.toContain(JSON.stringify(issueKeys.timeline("MUL-7")));
  });

  it("fetches a focused issue at once", async () => {
    renderLinks();
    fireEvent.focus(screen.getByText("Issue by id"));

    expect(fetched).toEqual([[...issueKeys.detail("ws-1", ISSUE_ID)]]);
    await vi.advanceTimersByTimeAsync(80);
    expect(keys()).toContain(JSON.stringify(issueKeys.timeline(ISSUE_ID)));
  });

  it("fetches nothing for issues a sweeping pointer only crosses", async () => {
    renderLinks();
    fireEvent.mouseEnter(screen.getByText("Issue by id"));
    await vi.advanceTimersByTimeAsync(20);
    fireEvent.mouseEnter(screen.getByText("Other issue"));
    await vi.advanceTimersByTimeAsync(80);

    expect(fetched).toEqual([[...issueKeys.detail("ws-1", OTHER_ID)]]);
    expect(keys()).toContain(JSON.stringify(issueKeys.timeline(OTHER_ID)));
    expect(keys().some((key) => key.includes(ISSUE_ID))).toBe(false);
  });

  it("drops what has not started when the pointer leaves or moves to another link", async () => {
    renderLinks();
    fireEvent.mouseEnter(screen.getByText("Issue by id"));
    await vi.advanceTimersByTimeAsync(50);
    fireEvent.mouseLeave(screen.getByText("Issue by id"));
    fireEvent.mouseEnter(screen.getByText("Other issue"));
    await vi.advanceTimersByTimeAsync(20);
    fireEvent.mouseEnter(screen.getByText("Settings"));
    await vi.advanceTimersByTimeAsync(200);

    // The first issue was rested on long enough to fetch, not to fill in.
    expect(fetched).toEqual([[...issueKeys.detail("ws-1", ISSUE_ID)]]);
    expect(prefetched).toHaveLength(0);
  });

  it("fetches a guest only the issue", async () => {
    renderLinks({ wsId: "ws-1", guest: true });
    fireEvent.mouseEnter(screen.getByText("Issue by id"));
    await vi.advanceTimersByTimeAsync(200);

    expect(fetched).toEqual([[...issueKeys.detail("ws-1", ISSUE_ID)]]);
    expect(keys().some((key) => key.includes(ISSUE_ID))).toBe(false);
  });

  it("leaves pages under an issue alone", async () => {
    renderLinks();
    fireEvent.mouseEnter(screen.getByText("Deliverable"));
    await vi.advanceTimersByTimeAsync(200);

    expect(fetched).toHaveLength(0);
    expect(prefetched).toHaveLength(0);
  });
});
