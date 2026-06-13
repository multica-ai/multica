import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { NavigationProvider } from "@multica/views/navigation";
import type { NavigationAdapter } from "@multica/views/navigation";
import { MobileShellFrame } from "./mobile-shell";

function renderShell(pathname = "/acme/m/issues") {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname,
    searchParams: new URLSearchParams(),
  };

  render(
    <NavigationProvider value={adapter}>
      <WorkspaceSlugProvider slug="acme">
        <MobileShellFrame>
          <main>route body</main>
        </MobileShellFrame>
      </WorkspaceSlugProvider>
    </NavigationProvider>,
  );

  return adapter;
}

describe("MobileShellFrame", () => {
  it("renders app chrome with current workspace and five bottom tabs", () => {
    renderShell("/acme/m/projects");

    expect(screen.getByText("acme")).toBeInTheDocument();
    expect(screen.getByText("route body")).toBeInTheDocument();

    const tabs = screen.getAllByRole("link").filter((link) =>
      link.getAttribute("aria-label")?.startsWith("Open mobile "),
    );
    expect(tabs).toHaveLength(5);
    expect(tabs.map((tab) => tab.getAttribute("href"))).toEqual([
      "/acme/m/kanban",
      "/acme/m/issues",
      "/acme/m/projects",
      "/acme/m/inbox",
      "/acme/m/settings",
    ]);

    expect(screen.getByRole("link", { current: "page" })).toHaveAttribute(
      "href",
      "/acme/m/projects",
    );
  });

  it("uses the navigation adapter for back navigation", async () => {
    const user = userEvent.setup();
    const adapter = renderShell();

    await user.click(screen.getByRole("button", { name: "Go back" }));

    expect(adapter.back).toHaveBeenCalledTimes(1);
  });

  it("opens a drawer with secondary routes", async () => {
    const user = userEvent.setup();
    renderShell();

    await user.click(screen.getByRole("button", { name: "Open mobile menu" }));

    expect(screen.getByRole("dialog", { name: "Mobile menu" })).toBeVisible();
    expect(screen.getByRole("link", { name: /Runtime/ })).toHaveAttribute(
      "href",
      "/acme/m/runtime",
    );
    expect(screen.getByRole("link", { name: /Chat/ })).toHaveAttribute(
      "href",
      "/acme/m/chat",
    );
  });
});
