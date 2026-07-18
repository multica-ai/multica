import "@testing-library/jest-dom/vitest";
import React from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { OperatingSystemNavItems } from "./cerebro-operating-system-nav";

const enabled = vi.hoisted(() => ({ value: true }));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => enabled.value }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "workspace-1" }) }));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: { terminology: { strategy: "Strategy", rock: "Rock", rocks: "Rocks" }, elements: [{ key: "meetings", enabled: true }, { key: "org_chart", enabled: true }] } }) })); // CEREBRO-PATCH(operating-system-stage4-nav): Cover enabled Stage 4 navigation.
vi.mock("@multica/cerebro-operating-system/core", () => ({ settingsOptions: () => ({ queryKey: ["settings"] }), elementsOptions: () => ({ queryKey: ["elements"] }) })); // CEREBRO-PATCH(operating-system-stage4-nav): Match the fork-owned query surface.
vi.mock("@multica/ui/components/ui/sidebar", () => ({
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <li>{children}</li>,
  SidebarMenuButton: ({ render, children }: { render: React.ReactElement; children: React.ReactNode }) =>
    React.cloneElement(render, {}, children),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ pathname: "/acme/rocks" }),
}));

describe("OperatingSystemNavItems", () => {
  it("links Rocks and Strategy when enabled", () => {
    render(<OperatingSystemNavItems workspaceSlug="acme" />);
    expect(screen.getByText("Operating System")).toBeInTheDocument();
    expect(screen.getByText("NEW")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Rocks" })).toHaveAttribute("href", "/acme/rocks");
    expect(screen.getByRole("link", { name: "Strategy" })).toHaveAttribute("href", "/acme/strategy");
    expect(screen.getByRole("link", { name: "Meetings" })).toHaveAttribute("href", "/acme/meetings"); // CEREBRO-PATCH(operating-system-stage4-nav): Verify Meetings link.
    expect(screen.getByRole("link", { name: "Org Chart" })).toHaveAttribute("href", "/acme/org-chart"); // CEREBRO-PATCH(operating-system-stage4-nav): Verify Org Chart link.
  });

  it("renders nothing when disabled", () => {
    enabled.value = false;
    const { container } = render(<OperatingSystemNavItems workspaceSlug="acme" />);
    expect(container).toBeEmptyDOMElement();
  });
});
