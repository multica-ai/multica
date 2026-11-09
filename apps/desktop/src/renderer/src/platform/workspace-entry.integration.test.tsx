import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, waitFor } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { bindWorkspaceSlugLookup, createQueryClient } from "@multica/core/query-client";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { linkKeys } from "@multica/core/links";
import { selectWorkspaceEntry } from "./workspace-selection";
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
    ["acme", "home"], ["links", "home"], ["acme", "none"],
    ["acme", "error"], ["acme", "timeout"],
  ])("existing %s issue-detail sessions select %s without losing tabs", async (slug, mode) => {
    const client = createQueryClient();
    unbind = bindWorkspaceSlugLookup(client);
    client.setQueryData(workspaceKeys.list(), [{ id: "ws", slug }, { id: "other", slug: "butter" }]);
    client.setQueryData(linkKeys.list("other"), []);
    const detail = `/${slug}/issues/ACME-1?tab=activity#comment-c1`;
    useTabStore.getState().switchWorkspace(slug, detail);
    useTabStore.getState().switchWorkspace("butter", "/butter/issues");
    const home = {
      id: "home", workspaceId: "ws", projectId: null, name: "Docs", icon: "",
      urlTemplate: "https://example.com", openMode: "embed", isHome: true,
      allowSameOrigin: false, position: 0,
    };
    // The home is configured after the issue-detail session was created.
    if (mode === "error") list.mockRejectedValue(new Error("offline"));
    else if (mode === "timeout") list.mockImplementation(() => new Promise(() => {}));
    else list.mockResolvedValue(mode === "home" ? [home] : []);
    initTabCoordinator();
    let navigation: ReturnType<typeof useNavigation>;
    function Probe() { navigation = useNavigation(); return null; }
    render(<QueryClientProvider client={client}><DesktopNavigationProvider><Probe /></DesktopNavigationProvider></QueryClientProvider>);
    const target = mode === "home" ? `/${slug}/links/home` : detail;
    for (let i = 0; i < 2; i++) {
      act(() => navigation.push(`/${slug}/start`));
      // Even a pending or failed fetch resumes immediately, never parking at start.
      expect(getActiveTab(useTabStore.getState())?.url).toBe(detail);
      act(() => useTabStore.getState().updateTab(getActiveTab(useTabStore.getState())!.id, { title: "Issue detail" }));
      if (mode === "timeout") await act(async () => { await new Promise((resolve) => setTimeout(resolve, 1_600)); });
      await waitFor(() => expect(getActiveTab(useTabStore.getState())?.url).toBe(target));
      await waitFor(() => expect(getAppRouter().state.location.pathname).toBe(target.split("?")[0]));
      expect(useTabStore.getState().byWorkspace[slug].tabs.map((tab) => tab.url))
        .toEqual(mode === "home" ? [detail, target] : [detail]);
      // Make issue detail active again to prove the next switch selects/dedupes home.
      act(() => useTabStore.getState().setActiveTab(useTabStore.getState().byWorkspace[slug].tabs[0].id));
      act(() => navigation.push("/butter/start"));
    }
    expect(list).toHaveBeenCalledWith(undefined, "ws");
    client.clear();
  });

  it.each([true, false])("initial selection of existing sessions is home-aware (home=%s)", async (isHome) => {
    const client = createQueryClient();
    client.setQueryData(workspaceKeys.list(), [{ id: "ws", slug: "acme" }]);
    useTabStore.getState().switchWorkspace("acme", "/acme/issues/ACME-1?tab=activity#comment-c1");
    useTabStore.setState({ activeWorkspaceSlug: null });
    list.mockResolvedValue(isHome ? [{
      id: "home", projectId: null, isHome: true, openMode: "embed", urlTemplate: "https://example.com",
    }] : []);
    initTabCoordinator();
    await act(async () => { await selectWorkspaceEntry(client, "acme"); });
    const target = isHome ? "/acme/links/home" : "/acme/issues/ACME-1?tab=activity#comment-c1";
    expect(getActiveTab(useTabStore.getState())?.url).toBe(target);
    await waitFor(() => expect(getAppRouter().state.location.pathname).toBe(target.split("?")[0]));
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(isHome ? 2 : 1);
    client.clear();
  });

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
