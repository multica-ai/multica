import React from "react";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { HelpLauncher } from "./help-launcher";

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ children, ...props }: React.ComponentProps<"button">) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({
    children,
    render,
    onClick,
  }: {
    children: React.ReactNode;
    render?: React.ReactElement;
    onClick?: () => void;
  }) =>
    render ? (
      React.cloneElement(render, undefined, children)
    ) : (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    ),
}));

describe("HelpLauncher", () => {
  it("links docs and changelog to the FurtherRef desktop site", () => {
    render(<HelpLauncher />);

    expect(screen.getByRole("link", { name: /docs/i })).toHaveAttribute(
      "href",
      "https://multica.furtherref.com/docs",
    );
    expect(screen.getByRole("link", { name: /change log/i })).toHaveAttribute(
      "href",
      "https://multica.furtherref.com/changelog",
    );
  });
});
