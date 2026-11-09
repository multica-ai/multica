import { act, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enIssues from "@multica/views/locales/en/issues.json";

const state = vi.hoisted(() => ({ authenticated: true, ready: true }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (value: { status: string }) => unknown) =>
    selector({ status: state.authenticated ? "authenticated" : "unauthenticated" }),
}));
vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({ ready: state.ready, workspaces: [{ slug: "acme" }, { slug: "other" }] }),
}));
const { route, error } = vi.hoisted(() => ({ route: vi.fn(), error: vi.fn() }));
vi.mock("./navigation", () => ({ routeContentLinkPath: route }));
vi.mock("sonner", () => ({ toast: { error } }));

import { IssueDeepLinkBridge } from "./issue-deep-link-bridge";

let receive: ((url: string) => void) | undefined;
const unsubscribe = vi.fn();
const subscribe = vi.fn((callback: (url: string) => void) => {
  receive = callback;
  return unsubscribe;
});
const ui = () => (
  <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
    <IssueDeepLinkBridge />
  </I18nProvider>
);
const link = (path: string, origin = "https://tasks.example") =>
  `multica://open/issue?url=${encodeURIComponent(origin + path)}`;

beforeEach(() => {
  vi.clearAllMocks();
  state.authenticated = true;
  state.ready = true;
  receive = undefined;
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: "https://tasks.example" } },
      onIssueOpen: subscribe,
    },
  });
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
    expect(route).toHaveBeenCalledWith("/other/issues/OTH-2#comment-c1", "foreground-tab");
    unmount();
    expect(unsubscribe).toHaveBeenCalled();
  });

  it("reports another instance without navigating", () => {
    render(ui());
    act(() => receive!(link("/acme/issues/ACM-1", "https://other.example")));
    expect(error).toHaveBeenCalledWith(enIssues.desktop.link_other_instance);
    expect(route).not.toHaveBeenCalled();
  });

  it("does not navigate into a workspace outside the current account", () => {
    render(ui());
    act(() => receive!(link("/private/issues/PRI-1")));
    expect(error).toHaveBeenCalledWith(enIssues.desktop.link_workspace_unavailable);
    expect(route).not.toHaveBeenCalled();
  });
});
