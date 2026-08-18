import type { ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { render as rtlRender, screen, fireEvent, type RenderOptions } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import { setClientIdentity } from "@multica/core/platform";
import { versionMismatchStore } from "@multica/core/version";
import enLayout from "../locales/en/layout.json";
import { ServerVersionBanner } from "./server-version-banner";

const TEST_RESOURCES = {
  en: { layout: enLayout },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function render(ui: React.ReactElement, options?: RenderOptions) {
  return rtlRender(ui, { wrapper: I18nWrapper, ...options });
}

afterEach(() => {
  configStore.getState().setServerVersion("");
  setClientIdentity(undefined);
  versionMismatchStore.setState({ dismissed: false });
});

describe("ServerVersionBanner", () => {
  it("renders the upgrade hint when the app is newer than the server", () => {
    setClientIdentity({ platform: "desktop", version: "0.9.0", os: "macos" });
    configStore.getState().setServerVersion("0.8.2");

    render(<ServerVersionBanner />);

    expect(screen.getByRole("status")).toHaveTextContent(
      "Server version (0.8.2) is older than this app (0.9.0)",
    );
  });

  it("renders on patch-only drift (app 0.4.9 + server 0.4.8, #5848)", () => {
    setClientIdentity({ platform: "desktop", version: "0.4.9", os: "macos" });
    configStore.getState().setServerVersion("0.4.8");

    render(<ServerVersionBanner />);

    expect(screen.getByRole("status")).toHaveTextContent(
      "Server version (0.4.8) is older than this app (0.4.9)",
    );
  });

  it("stays hidden when the server is newer or versions match", () => {
    setClientIdentity({ platform: "desktop", version: "0.8.0", os: "macos" });
    configStore.getState().setServerVersion("0.9.0");
    const { container, rerender } = render(<ServerVersionBanner />);
    expect(container).toBeEmptyDOMElement();

    configStore.getState().setServerVersion("0.8.4");
    rerender(<ServerVersionBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it("stays hidden when either version is unknown (cloud, dev builds)", () => {
    setClientIdentity({ platform: "web", version: "0.9.0", os: "macos" });
    // Cloud omits server_version entirely.
    const { container, rerender } = render(<ServerVersionBanner />);
    expect(container).toBeEmptyDOMElement();

    // Desktop dev build placeholder version.
    setClientIdentity({ platform: "desktop", version: "0.1.0", os: "macos" });
    configStore.getState().setServerVersion("0.8.0");
    rerender(<ServerVersionBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it("stays dismissed for the rest of the session", () => {
    setClientIdentity({ platform: "desktop", version: "0.9.0", os: "macos" });
    configStore.getState().setServerVersion("0.8.0");

    const { container, rerender } = render(<ServerVersionBanner />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    rerender(<ServerVersionBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
