import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

type ModalState = {
  modal: "issue-rename" | "create-squad" | null;
  data: Record<string, unknown> | null;
  close: ReturnType<typeof vi.fn>;
};

const listeners = new Set<() => void>();
const modalStore: ModalState = {
  modal: null,
  data: null,
  close: vi.fn(() => {
    modalStore.modal = null;
    modalStore.data = null;
    for (const listener of listeners) listener();
  }),
};

function setModal(modal: ModalState["modal"], data: Record<string, unknown> | null = null) {
  modalStore.modal = modal;
  modalStore.data = data;
  for (const listener of listeners) listener();
}

vi.mock("@multica/core/modals", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    useModalStore: Object.assign(
      <T,>(selector: (s: ModalState) => T) =>
        useSyncExternalStore(
          (cb) => {
            listeners.add(cb);
            return () => listeners.delete(cb);
          },
          () => selector(modalStore),
        ),
      { getState: () => modalStore },
    ),
  };
});

vi.mock("./rename-issue", () => ({
  RenameIssueModal: ({
    open,
    onClose,
    onExitComplete,
    data,
  }: {
    open?: boolean;
    onClose: () => void;
    onExitComplete?: () => void;
    data: Record<string, unknown> | null;
  }) => (
    <div data-testid="rename-modal" data-open={String(open)}>
      <span>{String(data?.title ?? "")}</span>
      <button type="button" onClick={onClose}>
        close
      </button>
      <button type="button" onClick={onExitComplete}>
        finish-exit
      </button>
    </div>
  ),
}));

vi.mock("./create-squad", () => ({
  CreateSquadModal: ({ open }: { open?: boolean }) => (
    <div data-testid="squad-modal" data-open={String(open)} />
  ),
}));

vi.mock("./limit-recovery-dialog", () => ({
  LimitRecoveryDialog: () => null,
}));

vi.mock("../extensions", () => ({
  getProjectFoldersExtension: () => null,
}));

import { ModalRegistry } from "./registry";

describe("ModalRegistry exit lifecycle", () => {
  beforeEach(() => {
    modalStore.modal = null;
    modalStore.data = null;
    modalStore.close.mockClear();
  });

  it("keeps the closing modal mounted with open=false until exit completes", () => {
    render(<ModalRegistry />);
    expect(screen.queryByTestId("rename-modal")).not.toBeInTheDocument();

    act(() => setModal("issue-rename", { title: "Fix exit" }));
    expect(screen.getByTestId("rename-modal")).toHaveAttribute("data-open", "true");
    expect(screen.getByText("Fix exit")).toBeInTheDocument();

    // Closing flips open to false without unmounting mid-animation.
    fireEvent.click(screen.getByRole("button", { name: "close" }));
    expect(screen.getByTestId("rename-modal")).toHaveAttribute("data-open", "false");
    expect(screen.getByText("Fix exit")).toBeInTheDocument();

    // Once the dialog signals exit complete, the modal unmounts cleanly.
    fireEvent.click(screen.getByRole("button", { name: "finish-exit" }));
    expect(screen.queryByTestId("rename-modal")).not.toBeInTheDocument();
  });

  it("ignores a stale exit callback when another modal opens immediately", () => {
    render(<ModalRegistry />);
    act(() => setModal("issue-rename", { title: "First" }));
    fireEvent.click(screen.getByRole("button", { name: "close" }));
    expect(screen.getByTestId("rename-modal")).toHaveAttribute("data-open", "false");

    act(() => setModal("create-squad"));
    expect(screen.queryByTestId("rename-modal")).not.toBeInTheDocument();
    expect(screen.getByTestId("squad-modal")).toHaveAttribute("data-open", "true");
  });
});
