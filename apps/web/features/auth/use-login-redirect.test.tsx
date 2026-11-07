import { act, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import type { ApiClient } from "@multica/core/api";
import { useLogout } from "@multica/views/auth";
import { NavigationProvider, type NavigationAdapter } from "@multica/views/navigation";
import { mockUser } from "@/test/helpers";
import { useLoginRedirect } from "./use-login-redirect";

const router = vi.hoisted(() => ({ push: vi.fn(), replace: vi.fn() }));

vi.mock("next/navigation", () => ({
  useRouter: () => router,
  usePathname: () => window.location.pathname,
}));

vi.mock("@multica/core/platform", () => ({
  clearClientSessionData: vi.fn(),
}));

const target = "/acme/issues/ACME-12?label=a#comment-123";
const original = window.location.href;
let store: ReturnType<typeof createAuthStore>;

function ProtectedPage({ enabled = true }: { enabled?: boolean }) {
  useLoginRedirect(enabled);
  const logout = useLogout();
  return <button onClick={logout}>Log out</button>;
}

function renderPage(enabled = true) {
  const adapter: NavigationAdapter = {
    ...router,
    back: vi.fn(),
    pathname: window.location.pathname,
    searchParams: new URLSearchParams(window.location.search),
    hash: window.location.hash,
    getShareableUrl: (path) => `https://example.com${path}`,
  };
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <NavigationProvider value={adapter}>
        <ProtectedPage enabled={enabled} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  window.history.replaceState(null, "", target);
  store = createAuthStore({
    api: { setToken: vi.fn() } as unknown as ApiClient,
    storage: { getItem: () => null, setItem: vi.fn(), removeItem: vi.fn() },
  });
  store.getState().setUser(mockUser);
  registerAuthStore(store);
});

afterEach(() => window.history.replaceState(null, "", original));

describe("useLoginRedirect", () => {
  it("keeps both logout navigations free of the previous account's destination", () => {
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Log out" }));
    expect(router.push).toHaveBeenCalledExactlyOnceWith("/login");
    expect(router.replace).toHaveBeenCalledExactlyOnceWith("/login");
  });

  it("retains the current destination after a server-rejected session", () => {
    renderPage();
    act(() => store.getState().sessionExpired());
    expect(router.replace).toHaveBeenCalledExactlyOnceWith(`/login?next=${encodeURIComponent(target)}`);
    expect(router.push).not.toHaveBeenCalled();
  });

  it("retains a newly opened link after an earlier explicit logout", () => {
    store.getState().logout();
    renderPage();
    expect(router.replace).toHaveBeenCalledExactlyOnceWith(`/login?next=${encodeURIComponent(target)}`);
  });

  it("does not redirect while authentication is unresolved", () => {
    store.setState({ user: null, isLoading: true, status: "authenticating" });
    renderPage();
    expect(router.replace).not.toHaveBeenCalled();
  });

  it("does not let a retained layout redirect after it no longer owns the route", () => {
    renderPage(false);
    act(() => store.getState().sessionExpired());
    expect(router.replace).not.toHaveBeenCalled();
  });
});
