import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// The shared Dialog is a Base UI portal that's awkward to test — strip it to
// simple pass-through wrappers. The typed-confirmation logic lives in the
// dialog body, not in Base UI, so this doesn't reduce coverage.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { DeleteWorkspaceDialog } from "./delete-workspace-dialog";

describe("DeleteWorkspaceDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("disables Delete when input is empty", () => {
    render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Delete workspace" })).toBeDisabled();
  });

  it("keeps Delete disabled when input doesn't match (case-sensitive)", async () => {
    const user = userEvent.setup();
    render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    await user.type(screen.getByRole("textbox"), "ACME"); // wrong case
    expect(screen.getByRole("button", { name: "Delete workspace" })).toBeDisabled();

    await user.clear(screen.getByRole("textbox"));
    await user.type(screen.getByRole("textbox"), "acme "); // trailing space
    expect(screen.getByRole("button", { name: "Delete workspace" })).toBeDisabled();
  });

  it("enables Delete on exact match and calls onConfirm when clicked", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={vi.fn()}
        onConfirm={onConfirm}
      />,
    );

    await user.type(screen.getByRole("textbox"), "acme");
    const deleteBtn = screen.getByRole("button", { name: "Delete workspace" });
    expect(deleteBtn).toBeEnabled();

    await user.click(deleteBtn);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("submits on Enter when matched; ignores Enter when not matched", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={vi.fn()}
        onConfirm={onConfirm}
      />,
    );

    const input = screen.getByRole("textbox");
    await user.type(input, "acm{Enter}"); // not yet matched
    expect(onConfirm).not.toHaveBeenCalled();

    await user.type(input, "e{Enter}"); // now matches "acme"
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("Cancel closes the dialog and does not call onConfirm", async () => {
    const user = userEvent.setup();
    const onOpenChange = vi.fn();
    const onConfirm = vi.fn();
    render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={onOpenChange}
        onConfirm={onConfirm}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("shows loading state and disables both buttons while pending", () => {
    render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        loading
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Deleting..." })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  });

  it("matches names with spaces, unicode, and other non-ASCII characters literally", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <DeleteWorkspaceDialog
        workspaceName="My 团队 🚀"
        open
        onOpenChange={vi.fn()}
        onConfirm={onConfirm}
      />,
    );
    const input = screen.getByRole("textbox");
    await user.type(input, "My 团队 🚀");
    expect(screen.getByRole("button", { name: "Delete workspace" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Delete workspace" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("resets the input when the workspace being deleted changes (e.g. rename mid-dialog)", () => {
    const { rerender } = render(
      <DeleteWorkspaceDialog
        workspaceName="old-name"
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    const input = screen.getByRole("textbox") as HTMLInputElement;
    // Simulate user typing (set value directly since userEvent.type would
    // lose focus across re-renders).
    input.value = "old-name";
    rerender(
      <DeleteWorkspaceDialog
        workspaceName="new-name"
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox")).toHaveValue("");
  });

  it("clears the input when reopened so prior attempts don't leak", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    await user.type(screen.getByRole("textbox"), "partial");
    expect(screen.getByRole("textbox")).toHaveValue("partial");

    // Simulate close → reopen (e.g. user canceled, then clicked Delete again)
    rerender(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open={false}
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    rerender(
      <DeleteWorkspaceDialog
        workspaceName="acme"
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    expect(screen.getByRole("textbox")).toHaveValue("");
  });
});
