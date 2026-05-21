import { describe, it, expect, beforeEach, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

// The viewing timezone flows: auth store `user.timezone` → useViewingTimezone()
// → every dashboard query key. This test pins that chain: when the stored
// timezone changes, the dashboard report query keys must change, which is
// what makes TanStack Query refetch under the new tz.

// Capture every queryKey passed to useQuery. queryOptions() inside the
// dashboard options builders runs for real, so the key is the production key.
const queryKeys = vi.hoisted(() => [] as unknown[][]);

// Squad list returned by `squadListOptions` — keyed `["workspaces", wsId,
// "squads"]`. Tests mutate `.current` to drive the squad filter, including
// the stale-squad case where the selected id is no longer in the list.
const squadsRef = vi.hoisted(
  () =>
    ({ current: [] }) as {
      current: Array<{ id: string; name: string; avatar_url: string | null }>;
    },
);

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey: unknown[] }) => {
      queryKeys.push(opts.queryKey);
      const k = opts.queryKey;
      if (k.length === 3 && k[0] === "workspaces" && k[2] === "squads") {
        return { data: squadsRef.current, isLoading: false };
      }
      return { data: undefined, isLoading: true };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const tzRef = vi.hoisted(() => ({ current: "UTC" as string | null }));

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { timezone: string | null } | null };
  const state = (): AuthState => ({ user: { timezone: tzRef.current } });
  const useAuthStore = Object.assign(
    (sel?: (s: AuthState) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/runtimes/custom-pricing-store", () => {
  const state = () => ({ pricings: {} });
  const useCustomPricingStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) =>
      sel ? sel(state()) : state(),
    { getState: state },
  );
  return { useCustomPricingStore };
});

import { DashboardPage } from "./dashboard-page";

describe("DashboardPage — viewing timezone drives the query key", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    cleanup();
  });

  // The `tz` segment is the last element of every dashboard key
  // (see dashboardKeys in @multica/core/dashboard/queries).
  function tzSegments(): unknown[] {
    return queryKeys
      .filter((k) => k[0] === "dashboard")
      .map((k) => k[k.length - 1]);
  }

  it("uses the stored timezone in every dashboard query key", () => {
    tzRef.current = "UTC";
    renderWithI18n(<DashboardPage />);

    const tzs = tzSegments();
    expect(tzs.length).toBeGreaterThan(0);
    expect(tzs.every((tz) => tz === "UTC")).toBe(true);
  });

  it("flips the query key when the stored timezone changes", () => {
    tzRef.current = "UTC";
    renderWithI18n(<DashboardPage />);
    const utcKeys = queryKeys.filter((k) => k[0] === "dashboard");

    queryKeys.length = 0;
    cleanup();

    tzRef.current = "Asia/Tokyo";
    renderWithI18n(<DashboardPage />);
    const tokyoKeys = queryKeys.filter((k) => k[0] === "dashboard");

    expect(utcKeys.length).toBe(tokyoKeys.length);
    expect(utcKeys.length).toBeGreaterThan(0);
    // Same number of dashboard queries, but no key is shared between the
    // two timezones — so TanStack Query treats every series as a fresh
    // fetch and refetches under the new tz.
    for (let i = 0; i < utcKeys.length; i++) {
      expect(utcKeys[i]).not.toEqual(tokyoKeys[i]);
    }
  });
});

describe("DashboardPage — squad filter", () => {
  beforeEach(() => {
    queryKeys.length = 0;
    squadsRef.current = [];
    cleanup();
  });

  // The squad segment sits at index length-2 of every dashboard key
  // (immediately before the tz segment) — see dashboardKeys in
  // @multica/core/dashboard/queries.
  function squadSegments(): unknown[] {
    return queryKeys
      .filter((k) => k[0] === "dashboard")
      .map((k) => k[k.length - 2]);
  }

  // The Select trigger carries `aria-labelledby`, so its accessible name is
  // not the visible text — pick it out of the combobox set by content.
  function squadTrigger(): HTMLElement {
    const trigger = screen
      .getAllByRole("combobox")
      .find((c) => /squad/i.test(c.textContent ?? ""));
    if (!trigger) throw new Error("squad filter trigger not found");
    return trigger;
  }

  it("renders the squad filter and threads a squad segment into every dashboard key", () => {
    tzRef.current = "UTC";
    const { getAllByText } = renderWithI18n(<DashboardPage />);

    // The squad filter trigger defaults to "All squads".
    expect(getAllByText("All squads").length).toBeGreaterThan(0);

    // It is null while "All squads" is selected — the key shape, not the
    // value, is what this pins.
    const segs = squadSegments();
    expect(segs.length).toBeGreaterThan(0);
    expect(segs.every((s) => s === null)).toBe(true);
  });

  it("threads the picked squad's id into every dashboard key", async () => {
    tzRef.current = "UTC";
    squadsRef.current = [
      { id: "squad-alpha", name: "Alpha Squad", avatar_url: null },
      { id: "squad-beta", name: "Beta Squad", avatar_url: null },
    ];
    const user = userEvent.setup();
    renderWithI18n(<DashboardPage />);

    await user.click(squadTrigger());
    const option = await screen.findByRole("option", { name: "Alpha Squad" });

    // Drop the keys emitted while the popup opened — only the keys from the
    // post-selection render should be asserted.
    queryKeys.length = 0;
    await user.click(option);

    const segs = squadSegments();
    expect(segs.length).toBeGreaterThan(0);
    expect(segs.every((s) => s === "squad-alpha")).toBe(true);
  });

  it("drops a stale squad selection back to null when the squad disappears", async () => {
    tzRef.current = "UTC";
    squadsRef.current = [
      { id: "squad-alpha", name: "Alpha Squad", avatar_url: null },
      { id: "squad-beta", name: "Beta Squad", avatar_url: null },
    ];
    const user = userEvent.setup();
    const { rerender } = renderWithI18n(<DashboardPage />);

    await user.click(squadTrigger());
    await user.click(await screen.findByRole("option", { name: "Alpha Squad" }));

    // Squad Alpha is deleted (or the workspace switched): the squad list no
    // longer contains it, but squadValue state still holds its id.
    squadsRef.current = [
      { id: "squad-beta", name: "Beta Squad", avatar_url: null },
    ];
    queryKeys.length = 0;
    rerender(<DashboardPage />);

    // The validation memo must collapse the stale id to null — otherwise
    // every query silently filters to empty rows behind a trigger that
    // still reads "All squads".
    const segs = squadSegments();
    expect(segs.length).toBeGreaterThan(0);
    expect(segs.every((s) => s === null)).toBe(true);
  });
});
