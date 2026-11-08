import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { bindWorkspaceSlugLookup, createQueryClient } from "@multica/core/query-client";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { getActiveTab, useTabStore } from "@/stores/tab-store";
import { useNavigation } from "@multica/views/navigation";
import { DesktopNavigationProvider } from "./navigation";
import { DesktopWorkspaceEntry } from "./workspace-entry";
import { __resetTabCoordinatorForTests, getAppRouter, initTabCoordinator } from "./tab-coordinator";

const list = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { listWorkspaceLinks: list } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", async (original) => ({
  ...await original<typeof import("@multica/core/paths")>(),
  useRequiredWorkspaceSlug: () => useTabStore.getState().activeWorkspaceSlug!,
}));
vi.mock("@multica/views/i18n", async (original) => ({
  ...await original<typeof import("@multica/views/i18n")>(),
  useT: () => ({ t: () => "Loading" }),
}));

let unbind = () => {};
beforeEach(() => {
  __resetTabCoordinatorForTests();
  useTabStore.getState().reset();
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: { runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } } },
  });
});
afterEach(() => {
  unbind();
  __resetTabCoordinatorForTests();
});

describe("desktop workspace entry integration", () => {
  it.each([
    ["links", true], ["links", false], ["dingtalk", true], ["dingtalk", false],
  ] as const)("rail first entry resolves %s (home=%s) and later resumes without new tabs", async (slug, isHome) => {
    const client = createQueryClient();
    unbind = bindWorkspaceSlugLookup(client);
    client.setQueryData(workspaceKeys.list(), [{ slug: "acme" }, { slug }]);
    list.mockResolvedValue(isHome ? [{
      id: "home", workspaceId: "ws", projectId: null, name: "Docs", icon: "",
      urlTemplate: "https://example.com", openMode: "embed", isHome: true,
      allowSameOrigin: false, position: 0,
    }] : []);
    useTabStore.getState().switchWorkspace("acme", "/acme/issues/ACME-1?tab=activity#comment-c1");
    initTabCoordinator();
    let navigation: ReturnType<typeof useNavigation>;
    function Probe() {
      navigation = useNavigation();
      const active = useTabStore((state) => getActiveTab(state)?.url);
      return active === `/${slug}/start` ? <DesktopWorkspaceEntry /> : null;
    }
    render(<QueryClientProvider client={client}><DesktopNavigationProvider><Probe /></DesktopNavigationProvider></QueryClientProvider>);
    act(() => navigation.push(`/${slug}/start`));
    expect(getActiveTab(useTabStore.getState())?.url).toBe(`/${slug}/start`);
    const target = isHome ? `/${slug}/links/home` : `/${slug}/issues`;
    await waitFor(() => expect(getActiveTab(useTabStore.getState())?.url).toBe(target));
    await waitFor(() => expect(getAppRouter().state.location.pathname).toBe(target));
    expect(getActiveTab(useTabStore.getState())?.history).toEqual({ stack: [target], index: 0 });
    for (let i = 0; i < 4; i++) {
      act(() => navigation.push("/acme/start"));
      expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/issues/ACME-1?tab=activity#comment-c1");
      act(() => navigation.push(`/${slug}/start`));
      expect(getActiveTab(useTabStore.getState())?.url).toBe(target);
    }
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(1);
    expect(useTabStore.getState().byWorkspace[slug].tabs).toHaveLength(1);
    client.clear();
  });
});
