import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import enLayout from "../locales/en/layout.json";
import { HelpLauncher } from "./help-launcher";

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
    DropdownMenuItem: ({ children }: { children: ReactNode }) => <>{children}</>,
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

  // MUL-4819: the version row's DropdownMenuLabel must sit inside a
  // DropdownMenuGroup. Rendering it bare made Base UI's Menu.GroupLabel throw
  // on open, unmounting the whole app (black screen, no error) because no error
  // boundary sits above the sidebar. Rendering here must not throw.
  it("renders the version row without a missing-group crash", () => {
    configStore.getState().setServerVersion("9.9.9");
    expect(() => render(<HelpLauncher />)).not.toThrow();
    expect(screen.getByText("Server version 9.9.9")).toBeInTheDocument();
  });
});
