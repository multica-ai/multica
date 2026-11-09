import { renderToString } from "react-dom/server";
import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const installUserTimingCap = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/diagnostics", () => ({ installUserTimingCap }));
vi.mock("@multica/core/platform", () => ({
  CoreProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/i18n/browser", () => ({
  createBrowserCookieLocaleAdapter: () => ({}),
}));
vi.mock("@/platform/navigation", () => ({
  WebNavigationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/platform/scroll-restoration", () => ({
  WebScrollRestorationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/platform/user-locale-sync", () => ({ useUserLocaleSyncEnabled: () => false }));

import { WebProviders } from "./web-providers";

const tree = (
  <WebProviders locale="en" resources={{}}>
    <span />
  </WebProviders>
);

afterEach(() => {
  vi.unstubAllEnvs();
  installUserTimingCap.mockClear();
});

describe("WebProviders user timing cap", () => {
  it("installs the cap in development", () => {
    vi.stubEnv("NODE_ENV", "development");
    render(tree);
    expect(installUserTimingCap).toHaveBeenCalledTimes(1);
  });

  it("does not install the cap in production", () => {
    vi.stubEnv("NODE_ENV", "production");
    render(tree);
    expect(installUserTimingCap).not.toHaveBeenCalled();
  });

  it("does not install the cap during server rendering", () => {
    vi.stubEnv("NODE_ENV", "development");
    renderToString(tree);
    expect(installUserTimingCap).not.toHaveBeenCalled();
  });
});
