import { cloneElement, type ReactElement, type ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import enLayout from "../locales/en/layout.json";
import { isDesktopShell } from "../platform/local-directory";
import { HelpLauncher } from "./help-launcher";

// The download entry is gated on the desktop-shell probe, which reads a
// preload-injected bridge that jsdom never has. Mock it so both platforms are
// reachable from the same suite.
vi.mock("../platform/local-directory", () => ({
  isDesktopShell: vi.fn(() => false),
}));

// react-i18next isn't initialised in the views test env, so resolve the
// selector against the real en/layout.json to assert on actual copy.
vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      sel: (r: typeof enLayout) => string,
      vars?: Record<string, string>,
    ) => {
      const template = sel(enLayout);
      return vars
        ? template.replace(/\{\{(\w+)\}\}/g, (_, key) => String(vars[key] ?? ""))
        : template;
    },
  }),
}));

// Follows the app-sidebar.test.tsx convention of flattening the Base UI
// dropdown primitives to plain children so the menu content is always in
// the DOM, instead of exercising the real portal/open-state interaction.
//
// The mock deliberately preserves ONE real invariant: DropdownMenuLabel wraps
// Base UI's Menu.GroupLabel, whose useMenuGroupRootContext() throws when it has
// no Menu.Group ancestor. A plain-<div> mock silently swallowed that contract,
// which is exactly how MUL-4819 shipped — a version row rendered outside a
// DropdownMenuGroup crashed the whole app (no error boundary above the sidebar)
// the moment the Help menu opened. Mirroring the throw here keeps the guard.
// The group context lives inside the factory so it survives vi.mock hoisting.
vi.mock("@multica/ui/components/ui/dropdown-menu", async () => {
  const { createContext, useContext } = await import("react");
  const GroupContext = createContext(false);
  return {
    DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
    DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
    // Base UI's `render` prop swaps in a caller-supplied element (here the
    // <a>) and adopts the item's children. Flattening it to a bare fragment —
    // as this mock originally did — would drop the anchor entirely and make an
    // href assertion silently unfalsifiable.
    DropdownMenuItem: ({
      children,
      render,
    }: {
      children: ReactNode;
      render?: ReactElement;
    }) => (render ? cloneElement(render, undefined, children) : <>{children}</>),
    DropdownMenuGroup: ({ children }: { children: ReactNode }) => (
      <GroupContext.Provider value={true}>{children}</GroupContext.Provider>
    ),
    DropdownMenuLabel: ({ children }: { children: ReactNode }) => {
      if (!useContext(GroupContext)) {
        throw new Error(
          "Base UI: MenuGroupRootContext is missing. Menu group parts must be used within <Menu.Group>.",
        );
      }
      return <div>{children}</div>;
    },
    DropdownMenuSeparator: () => null,
    DropdownMenuTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  };
});

beforeEach(() => {
  vi.mocked(isDesktopShell).mockReturnValue(false);
});

afterEach(() => {
  configStore.getState().setServerVersion("");
});

describe("HelpLauncher", () => {
  it("does not show a version row when the server omits it", () => {
    render(<HelpLauncher />);
    expect(screen.queryByText(/Server version/)).not.toBeInTheDocument();
  });

  it("shows the server version once /api/config resolves it", () => {
    configStore.getState().setServerVersion("1.2.3");
    render(<HelpLauncher />);
    expect(screen.getByText("Server version 1.2.3")).toBeInTheDocument();
  });

  // MUL-6462: after web onboarding the desktop download CTA was unreachable —
  // no entry anywhere in the app, so users had to remember the URL or detour
  // through the marketing site. The Help menu is the persistent home for it.
  it("links to the download page on web", () => {
    render(<HelpLauncher />);
    const link = screen.getByRole("link", { name: /Desktop app/ });
    expect(link).toHaveAttribute("href", "https://multica.ai/download");
  });

  // AppSidebar is shared: apps/desktop renders the same component tree. Without
  // this gate the desktop app would offer to download the desktop app.
  it("hides the download entry inside the desktop shell", () => {
    vi.mocked(isDesktopShell).mockReturnValue(true);
    render(<HelpLauncher />);
    expect(screen.queryByText("Desktop app")).not.toBeInTheDocument();
    // The rest of the menu is unaffected by the gate.
    expect(screen.getByText("Docs")).toBeInTheDocument();
  });
});
