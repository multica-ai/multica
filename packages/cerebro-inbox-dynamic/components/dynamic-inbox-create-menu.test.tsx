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
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
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

    await user.click(screen.getByRole("button", { name: "New message" }));
    await user.click(screen.getByRole("button", { name: "New issue" }));
    await user.click(screen.getByRole("button", { name: "New reminder" }));

    expect(onNewMessage).toHaveBeenCalledTimes(1);
    expect(onNewIssue).toHaveBeenCalledTimes(1);
    expect(onNewReminder).toHaveBeenCalledTimes(1);
  });
});
