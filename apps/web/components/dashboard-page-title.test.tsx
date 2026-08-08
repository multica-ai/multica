import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";

const routeState = vi.hoisted(() => ({
  pathname: "/scaling-forever/issues/SCA-289",
  search: "",
}));

vi.mock("next/navigation", () => ({
  usePathname: () => routeState.pathname,
  useSearchParams: () => new URLSearchParams(routeState.search),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQuery: () => ({ data: undefined }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => null,
}));

import { DashboardPageTitle } from "./dashboard-page-title";
import { PUBLIC_DEFAULT_TITLE } from "@/lib/public-page-title";

describe("DashboardPageTitle", () => {
  afterEach(() => {
    cleanup();
    document.title = "";
    routeState.pathname = "/scaling-forever/issues/SCA-289";
    routeState.search = "";
  });

  it("uses an issue identifier from the path before workspace and issue data resolve", () => {
    document.title = PUBLIC_DEFAULT_TITLE;

    render(<DashboardPageTitle />);

    expect(document.title).toBe("SCA-289");
  });

  it("titles settings from the resolved tab, including the legacy lark alias", () => {
    routeState.pathname = "/acme/settings";
    routeState.search = "tab=lark";
    document.title = PUBLIC_DEFAULT_TITLE;

    render(<DashboardPageTitle />);

    expect(document.title).toBe("Settings · Integrations");
  });
});
