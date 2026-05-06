import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// The shared Dialog is a Base UI portal that's awkward to test under jsdom.
// Strip it to pass-throughs — the typed-confirmation logic lives in the body
// of the dialog, not in Base UI, so this loses no coverage.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) =>
    open ? <div>{children}</div> : null,
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { ResetLocalDataDialog, RESET_CONFIRMATION_PHRASE } from "./reset-local-data-dialog";

describe("ResetLocalDataDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("disables Reset when input is empty", () => {
    render(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: /reset local data/i }),
    ).toBeDisabled();
  });

  it("keeps Reset disabled when the input does not exactly match", async () => {
    const user = userEvent.setup();
    render(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    await user.type(screen.getByRole("textbox"), "RESET LOCAL DATA"); // wrong case
    expect(
      screen.getByRole("button", { name: /reset local data/i }),
    ).toBeDisabled();

    await user.clear(screen.getByRole("textbox"));
    await user.type(screen.getByRole("textbox"), "reset local data "); // trailing space
    expect(
      screen.getByRole("button", { name: /reset local data/i }),
    ).toBeDisabled();
  });

  it("enables Reset on exact match and fires onConfirm when clicked", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={onConfirm}
      />,
    );

    await user.type(screen.getByRole("textbox"), RESET_CONFIRMATION_PHRASE);
    const resetBtn = screen.getByRole("button", { name: /reset local data/i });
    expect(resetBtn).toBeEnabled();

    await user.click(resetBtn);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("submits on Enter when matched", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={onConfirm}
      />,
    );

    const input = screen.getByRole("textbox");
    await user.type(input, "reset local dat{Enter}"); // not yet matching
    expect(onConfirm).not.toHaveBeenCalled();

    await user.type(input, "a{Enter}");
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("Cancel closes the dialog and does not invoke onConfirm", async () => {
    const user = userEvent.setup();
    const onOpenChange = vi.fn();
    const onConfirm = vi.fn();
    render(
      <ResetLocalDataDialog
        open
        onOpenChange={onOpenChange}
        onConfirm={onConfirm}
      />,
    );

    await user.click(screen.getByRole("button", { name: /cancel/i }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("disables both buttons while loading and shows the busy label", () => {
    render(
      <ResetLocalDataDialog
        loading
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: /resetting/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /cancel/i })).toBeDisabled();
  });

  it("clears the input when the dialog reopens so prior typing does not leak", async () => {
    const user = userEvent.setup();
    const { rerender } = render(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    await user.type(screen.getByRole("textbox"), "partial");
    expect(screen.getByRole("textbox")).toHaveValue("partial");

    rerender(
      <ResetLocalDataDialog
        open={false}
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );
    rerender(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    expect(screen.getByRole("textbox")).toHaveValue("");
  });

  it("lists what will and will NOT be deleted so the user makes an informed choice", () => {
    render(
      <ResetLocalDataDialog
        open
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    // Body should mention the deleted-data buckets.
    expect(screen.getAllByText(/postgres/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/logs/i).length).toBeGreaterThan(0);
    // And the explicit safe-list (repos / preferences / OS credentials).
    expect(screen.getByText(/repository checkouts/i)).toBeInTheDocument();
    expect(screen.getByText(/preferences/i)).toBeInTheDocument();
  });
});
