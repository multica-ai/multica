// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cloneElement } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { DynamicInboxCreateMenu } from "./dynamic-inbox-create-menu";

afterEach(() => cleanup());

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({
    children,
    render,
  }: {
    children: React.ReactNode;
    render: React.ReactElement;
  }) => <>{render ? cloneElement(render, undefined, children) : children}</>,
  DropdownMenuContent: ({
    children,
    className,
  }: {
    children: React.ReactNode;
    className?: string;
  }) => (
    <div data-testid="create-menu-content" className={className}>
      {children}
    </div>
  ),
  DropdownMenuLabel: ({
    children,
    className,
  }: {
    children: React.ReactNode;
    className?: string;
  }) => <div className={className}>{children}</div>,
  DropdownMenuItem: ({
    children,
    className,
    onClick,
  }: {
    children: React.ReactNode;
    className?: string;
    onClick?: () => void;
  }) => (
    <button type="button" className={className} onClick={onClick}>
      {children}
    </button>
  ),
}));

describe("DynamicInboxCreateMenu", () => {
  it("opens each create flow from the menu", async () => {
    const user = userEvent.setup();
    const onNewMessage = vi.fn();
    const onNewIssue = vi.fn();
    const onNewReminder = vi.fn();

    render(
      <DynamicInboxCreateMenu
        onNewMessage={onNewMessage}
        onNewIssue={onNewIssue}
        onNewReminder={onNewReminder}
      />,
    );

    await user.click(screen.getByRole("button", { name: /New message/ }));
    await user.click(screen.getByRole("button", { name: /New issue/ }));
    await user.click(screen.getByRole("button", { name: /New reminder/ }));

    expect(onNewMessage).toHaveBeenCalledTimes(1);
    expect(onNewIssue).toHaveBeenCalledTimes(1);
    expect(onNewReminder).toHaveBeenCalledTimes(1);
  });

  it("shows a description under each create action", () => {
    render(
      <DynamicInboxCreateMenu
        onNewMessage={vi.fn()}
        onNewIssue={vi.fn()}
        onNewReminder={vi.fn()}
      />,
    );

    expect(screen.getByText("Send a direct message")).toBeTruthy();
    expect(screen.getByText("Create a task or ticket")).toBeTruthy();
    expect(screen.getByText("Get nudged at a set time")).toBeTruthy();
  });

  it("uses a mobile-friendly menu size and touch targets", () => {
    render(
      <DynamicInboxCreateMenu
        onNewMessage={vi.fn()}
        onNewIssue={vi.fn()}
        onNewReminder={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Create" }).className).toContain("h-10");
    expect(screen.getByRole("button", { name: "Create" }).className).toContain("min-w-10");
    expect(screen.getByTestId("create-menu-content").className).toContain(
      "w-[min(calc(100vw-2rem),20rem)]",
    );
  });

  it("can render as a floating inbox opener", () => {
    render(
      <DynamicInboxCreateMenu
        variant="floating"
        onNewMessage={vi.fn()}
        onNewIssue={vi.fn()}
        onNewReminder={vi.fn()}
      />,
    );

    const triggerClassName = screen.getByRole("button", { name: "Create" }).className;
    expect(triggerClassName).toContain("h-14");
    expect(triggerClassName).toContain("w-14");
    expect(triggerClassName).toContain("rounded-full");
  });
});
