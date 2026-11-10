import { act, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "@multica/views/locales/en/issues.json";
import enLayout from "@multica/views/locales/en/layout.json";
import type { ReactNode } from "react";
import type {
  WorkspacePathOptions,
  WorkspacePathRequest,
} from "../../../shared/workspace-window";

const { state, authState } = vi.hoisted(() => {
  const state = { authenticated: true, ready: true };
  return {
    state,
    authState: () => ({
      status: state.authenticated ? "authenticated" : "unauthenticated",
      user: state.authenticated ? { id: "user-1" } : null,
    }),
  };
});
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (value: ReturnType<typeof authState>) => unknown) => selector(authState()),
    { getState: authState },
  ),
}));
vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({ ready: state.ready, workspaces: [{ slug: "acme" }, { slug: "other" }] }),
}));
const { error } = vi.hoisted(() => ({ error: vi.fn() }));
vi.mock("sonner", () => ({ toast: { error } }));

import { IssueDeepLinkBridge } from "./issue-deep-link-bridge";
import { WorkspaceWindowBridge } from "./workspace-window-bridge";
import { routeContentLinkPath } from "./navigation";
import { getActiveTab, useTabStore } from "@/stores/tab-store";

let receive: ((url: string) => void) | undefined;
let receiveWorkspace: ((request: WorkspacePathRequest) => void) | undefined;
const focusWorkspaceWindow = vi.fn(
  (_slug: string, _path?: string, _options?: WorkspacePathOptions) => false,
);
const unsubscribe = vi.fn();
const subscribe = vi.fn((callback: (url: string) => void) => {
  receive = callback;
  return unsubscribe;
});
const ui = (children: ReactNode = <IssueDeepLinkBridge />) => (
  <I18nProvider locale="en" resources={{ en: { issues: enIssues, layout: enLayout } }}>
    {children}
  </I18nProvider>
);
const link = (path: string, origin = "https://tasks.example") =>
  `multica://open/issue?url=${encodeURIComponent(origin + path)}`;

beforeEach(() => {
  vi.clearAllMocks();
  state.authenticated = true;
  state.ready = true;
  receive = undefined;
  receiveWorkspace = undefined;
  focusWorkspaceWindow.mockReturnValue(false);
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: "https://tasks.example" } },
      onIssueOpen: subscribe,
      focusWorkspaceWindow,
      onOpenWorkspacePath: (callback: (request: WorkspacePathRequest) => void) => {
        receiveWorkspace = callback;
        return () => { receiveWorkspace = undefined; };
      },
    },
  });
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
});

describe("IssueDeepLinkBridge", () => {
  it("leaves links in main's queue until sign-in and membership are ready", () => {
    state.authenticated = false;
    const { rerender } = render(ui());
    expect(subscribe).not.toHaveBeenCalled();
    state.authenticated = true;
    state.ready = false;
    rerender(ui());
    expect(subscribe).not.toHaveBeenCalled();
    state.ready = true;
    rerender(ui());
    expect(subscribe).toHaveBeenCalled();
  });

  it("opens a cross-workspace issue with its comment through the navigation flow", () => {
    const { unmount } = render(ui());
    act(() => receive!(link("/other/issues/OTH-2#comment-c1")));
    expect(useTabStore.getState().activeWorkspaceSlug).toBe("other");
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/other/issues/OTH-2#comment-c1");
    unmount();
    expect(unsubscribe).toHaveBeenCalled();
  });

  it.each(["acme", "other"])("navigates an existing %s issue tab to the new comment", (slug) => {
    const oldPath = `/${slug}/issues/ACM-42#comment-old`;
    const newPath = `/${slug}/issues/ACM-42#comment-new`;
    useTabStore.getState().switchWorkspace(slug, oldPath);
    const existing = getActiveTab(useTabStore.getState())!;
    const tabCount = useTabStore.getState().byWorkspace[slug]!.tabs.length;
    if (slug !== "acme") useTabStore.getState().switchWorkspace("acme");
    render(ui());

    act(() => receive!(link(newPath)));

    const current = getActiveTab(useTabStore.getState())!;
    expect(current.id).toBe(existing.id);
    expect(current.url).toBe(newPath);
    expect(current.history.stack[current.history.index]).toBe(newPath);
    expect(useTabStore.getState().byWorkspace[slug]!.tabs).toHaveLength(tabCount);
  });

  it("applies the new comment in the workspace's existing window without moving the source", () => {
    const oldPath = "/other/issues/OTH-2#comment-old";
    const newPath = "/other/issues/OTH-2#comment-new";
    useTabStore.getState().switchWorkspace("other", oldPath);
    const destination = useTabStore.getState();
    const existing = getActiveTab(destination)!;
    // Each window has its own store. Save the destination's real sessions,
    // then restore them when delivering the request across the IPC boundary.
    useTabStore.getState().reset();
    useTabStore.getState().switchWorkspace("acme");
    const source = getActiveTab(useTabStore.getState());
    focusWorkspaceWindow.mockReturnValue(true);
    const { unmount } = render(ui());

    act(() => receive!(link(newPath)));

    expect(focusWorkspaceWindow).toHaveBeenCalledWith("other", newPath, { navigate: true });
    expect(getActiveTab(useTabStore.getState())).toBe(source);
    expect(useTabStore.getState().byWorkspace.other).toBeUndefined();
    const [, path, options] = focusWorkspaceWindow.mock.calls[0]!;
    unmount();
    useTabStore.setState({
      activeWorkspaceSlug: destination.activeWorkspaceSlug,
      byWorkspace: destination.byWorkspace,
    });
    render(ui(<WorkspaceWindowBridge />));

    act(() => receiveWorkspace!({ path: path!, ...options }));

    const current = getActiveTab(useTabStore.getState())!;
    expect(current.id).toBe(existing.id);
    expect(current.url).toBe(newPath);
    expect(current.history.stack[current.history.index]).toBe(newPath);
    expect(useTabStore.getState().byWorkspace.other!.tabs).toHaveLength(1);
  });

  it.each(["acme", "other"])("preserves the existing %s view on ordinary tab opens", (slug) => {
    const oldPath = `/${slug}/issues/ACM-42?view=activity#comment-old`;
    const newPath = `/${slug}/issues/ACM-42?view=activity#comment-new`;
    useTabStore.getState().switchWorkspace(slug, oldPath);
    const existing = getActiveTab(useTabStore.getState());
    useTabStore.getState().switchWorkspace("acme");

    routeContentLinkPath(newPath, "foreground-tab");

    expect(getActiveTab(useTabStore.getState())).toBe(existing);
  });

  it("preserves the destination's view for an ordinary request from another window", () => {
    const oldPath = "/other/issues/OTH-2?view=activity#comment-old";
    useTabStore.getState().switchWorkspace("other", oldPath);
    const existing = getActiveTab(useTabStore.getState());
    render(ui(<WorkspaceWindowBridge />));

    act(() => receiveWorkspace!({ path: "/other/issues/OTH-2?view=activity#comment-new" }));

    expect(getActiveTab(useTabStore.getState())).toBe(existing);
  });

  it("reports another instance without navigating", () => {
    render(ui());
    const previous = getActiveTab(useTabStore.getState());
    act(() => receive!(link("/acme/issues/ACM-1", "https://other.example")));
    expect(error).toHaveBeenCalledWith(enIssues.desktop.link_other_instance);
    expect(getActiveTab(useTabStore.getState())).toBe(previous);
  });

  it("does not navigate into a workspace outside the current account", () => {
    render(ui());
    const previous = getActiveTab(useTabStore.getState());
    act(() => receive!(link("/private/issues/PRI-1")));
    expect(error).toHaveBeenCalledWith(enIssues.desktop.link_workspace_unavailable);
    expect(getActiveTab(useTabStore.getState())).toBe(previous);
  });
});
