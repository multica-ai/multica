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
// dropdown primitives to plain children so the menu content is always in the
// DOM, instead of exercising the real portal/open-state interaction.
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
  configStore.getState().setFrontendBaseline("");
  configStore.getState().setBackendBaseline("");
  // Force the loading -> settled transition back to loading so each test
  // sees the initial loading state on the backend row.
  configStore.setState({ backendBaselineStatus: "loading" });
});

describe("HelpLauncher provenance rows", () => {
  it("renders two rows (frontend + backend) even when both values are unavailable", () => {
    render(<HelpLauncher />);
    expect(screen.getByText(enLayout.help.frontend_label)).toBeInTheDocument();
    expect(screen.getByText(enLayout.help.backend_label)).toBeInTheDocument();
    expect(
      screen.getAllByText(enLayout.help.frontend_unavailable).length,
    ).toBeGreaterThan(0);
    // Backend defaults to "loading" before the non-blocking /api/config resolves.
    expect(
      screen.getByText(enLayout.help.backend_loading),
    ).toBeInTheDocument();
  });

  it("shows the frontend and backend tags side by side when both are stamped", () => {
    configStore.getState().setFrontendBaseline("v0.4.2");
    configStore.getState().setBackendBaseline("v0.4.2");
    render(<HelpLauncher />);
    const tags = screen.getAllByText("v0.4.2");
    expect(tags.length).toBe(2);
  });

  it("shows two different tags when frontend and backend are out of sync", () => {
    configStore.getState().setFrontendBaseline("v0.4.2");
    configStore.getState().setBackendBaseline("v0.4.1");
    render(<HelpLauncher />);
    expect(screen.getByText("v0.4.2")).toBeInTheDocument();
    expect(screen.getByText("v0.4.1")).toBeInTheDocument();
  });

  it("shows the backend tag alongside an unavailable frontend", () => {
    configStore.getState().setFrontendBaseline("");
    configStore.getState().setBackendBaseline("v0.4.2");
    render(<HelpLauncher />);
    expect(
      screen.getByText(enLayout.help.frontend_unavailable),
    ).toBeInTheDocument();
    expect(screen.getByText("v0.4.2")).toBeInTheDocument();
  });

  it("shows the frontend tag alongside a loading backend", () => {
    configStore.getState().setFrontendBaseline("v0.4.2");
    configStore.setState({ backendBaselineStatus: "loading" });
    render(<HelpLauncher />);
    expect(screen.getByText("v0.4.2")).toBeInTheDocument();
    expect(
      screen.getByText(enLayout.help.backend_loading),
    ).toBeInTheDocument();
  });

  it("shows both rows as unavailable after a config failure settles", () => {
    configStore.getState().setFrontendBaseline("");
    configStore.getState().setBackendBaseline(); // settled, unavailable
    render(<HelpLauncher />);
    expect(
      screen.getByText(enLayout.help.frontend_unavailable),
    ).toBeInTheDocument();
    expect(
      screen.getByText(enLayout.help.backend_unavailable),
    ).toBeInTheDocument();
  });

  // MUL-4819 (kept as a guardrail): both DropdownMenuLabels must sit inside a
  // DropdownMenuGroup. Rendering either outside a group would crash the app on
  // open. The DropdownMenuLabel mock throws if it has no Group ancestor.
  it("does not throw when rendering both rows under a DropdownMenuGroup", () => {
    configStore.getState().setFrontendBaseline("v0.4.2");
    configStore.getState().setBackendBaseline("v0.4.2");
    expect(() => render(<HelpLauncher />)).not.toThrow();
  });
});