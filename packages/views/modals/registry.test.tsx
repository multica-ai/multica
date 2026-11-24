import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useModalStore } from "@multica/core/modals";

const captured = vi.hoisted(() => ({
  calls: [] as Array<{
    open?: boolean;
    onOpenChangeComplete?: (open: boolean) => void;
    data?: Record<string, unknown> | null;
  }>,
  mounts: 0,
}));

vi.mock("./rename-issue", async () => {
  const React = await import("react");
  return {
    RenameIssueModal: ({
      open,
      onOpenChangeComplete,
      data,
    }: {
      open?: boolean;
      onOpenChangeComplete?: (open: boolean) => void;
      data?: Record<string, unknown> | null;
    }) => {
      React.useEffect(() => {
        captured.mounts += 1;
      }, []);
      captured.calls.push({ open, onOpenChangeComplete, data });
      return (
        <div
          data-testid="mock-rename-issue"
          data-open={String(open)}
          data-title={String(data?.title ?? "")}
        />
      );
    },
  };
});

vi.mock("./limit-recovery-dialog", () => ({
  LimitRecoveryDialog: () => null,
}));

vi.mock("../extensions", () => ({
  getProjectFoldersExtension: () => null,
}));

import { ModalRegistry } from "./registry";

describe("ModalRegistry exit animation retention", () => {
  beforeEach(() => {
    captured.calls = [];
    captured.mounts = 0;
    useModalStore.setState({ modal: null, data: null });
  });

  afterEach(() => {
    cleanup();
    useModalStore.setState({ modal: null, data: null });
  });

  it("keeps the closing modal mounted with open=false until onOpenChangeComplete fires", () => {
    render(<ModalRegistry />);
    expect(screen.queryByTestId("mock-rename-issue")).not.toBeInTheDocument();

    act(() => {
      useModalStore.getState().open("issue-rename", { issueId: "1", title: "Original" });
    });

    const el = screen.getByTestId("mock-rename-issue");
    expect(el).toHaveAttribute("data-open", "true");
    expect(el).toHaveAttribute("data-title", "Original");

    // Closing flips open to false without unmounting immediately so exit animation can play.
    act(() => {
      useModalStore.getState().close();
    });

    const closingEl = screen.getByTestId("mock-rename-issue");
    expect(closingEl).toHaveAttribute("data-open", "false");
    expect(closingEl).toHaveAttribute("data-title", "Original");

    // Unmounts only once Base UI signals the exit transition finished.
    const complete = captured.calls.at(-1)?.onOpenChangeComplete;
    expect(complete).toBeTypeOf("function");
    act(() => {
      complete?.(false);
    });

    expect(screen.queryByTestId("mock-rename-issue")).not.toBeInTheDocument();
  });

  it("remounts cleanly on rapid close-then-reopen and ignores stale exit callbacks", () => {
    render(<ModalRegistry />);

    act(() => {
      useModalStore.getState().open("issue-rename", { issueId: "1", title: "First" });
    });
    expect(captured.mounts).toBe(1);

    act(() => {
      useModalStore.getState().close();
    });
    const staleComplete = captured.calls.at(-1)?.onOpenChangeComplete;

    // Reopen before the previous exit callback fires.
    act(() => {
      useModalStore.getState().open("issue-rename", { issueId: "1", title: "First" });
    });
    expect(captured.mounts).toBe(2);
    expect(screen.getByTestId("mock-rename-issue")).toHaveAttribute("data-open", "true");

    // Stale completion callback from the previous instance must not unmount the new modal.
    act(() => {
      staleComplete?.(false);
    });
    expect(screen.getByTestId("mock-rename-issue")).toHaveAttribute("data-open", "true");
  });
});
