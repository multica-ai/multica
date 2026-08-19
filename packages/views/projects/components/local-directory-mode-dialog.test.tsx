// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { LocalDirectoryExecutionMode } from "@multica/core/types";
import enProjects from "../../locales/en/projects.json";
import enCommon from "../../locales/en/common.json";
import { LocalDirectoryModeDialog } from "./local-directory-mode-dialog";
import type { WorktreeUnavailableReason } from "./local-directory-mode-dialog";

const TEST_RESOURCES = { en: { projects: enProjects, common: enCommon } };

function renderDialog(
  overrides: {
    value?: LocalDirectoryExecutionMode;
    unavailableReason?: WorktreeUnavailableReason;
    errorMessage?: string;
    onConfirm?: (mode: LocalDirectoryExecutionMode) => void;
  } = {},
) {
  const onConfirm = overrides.onConfirm ?? vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <LocalDirectoryModeDialog
        open
        onOpenChange={() => {}}
        path="/Users/dev/work/game-client"
        value={overrides.value ?? "in_place"}
        unavailableReason={overrides.unavailableReason}
        errorMessage={overrides.errorMessage}
        confirmLabel="Save"
        onConfirm={onConfirm}
      />
    </I18nProvider>,
  );
  return { onConfirm };
}

function worktreeOption(): HTMLElement {
  return screen.getAllByRole("radio")[1] as HTMLElement;
}

describe("LocalDirectoryModeDialog", () => {
  it("leads with what the user gets back, not the mode identifiers", () => {
    renderDialog();
    // The identifiers stay visible as a secondary hint for anyone cross-
    // referencing the CLI or docs, but the decision is framed by outcome.
    expect(screen.getByText("Edit this folder directly")).toBeTruthy();
    expect(screen.getByText("Run in parallel, isolated")).toBeTruthy();
    expect(screen.getByText("in_place")).toBeTruthy();
    expect(screen.getByText("worktree")).toBeTruthy();
    expect(screen.getByText("/Users/dev/work/game-client")).toBeTruthy();
  });

  it("marks the current mode as selected", () => {
    renderDialog({ value: "worktree" });
    expect(worktreeOption().getAttribute("aria-checked")).toBe("true");
    expect(screen.getAllByRole("radio")[0]?.getAttribute("aria-checked")).toBe(
      "false",
    );
  });

  it("confirms the newly picked mode, not the one it opened with", () => {
    const onConfirm = vi.fn();
    renderDialog({ value: "in_place", onConfirm });

    fireEvent.click(worktreeOption());
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(onConfirm).toHaveBeenCalledWith("worktree");
  });

  // A non-git folder cannot produce a branch, so offering the option would
  // guarantee the user's first task fails. Disable it where they choose.
  it("disables parallel mode for a non-git folder and says why", () => {
    const onConfirm = vi.fn();
    renderDialog({ unavailableReason: "not_git", onConfirm });

    const option = worktreeOption();
    expect(option.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/not a git repository/i)).toBeTruthy();

    fireEvent.click(option);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    // Still the mode it opened with — the disabled option cannot be selected.
    expect(onConfirm).toHaveBeenCalledWith("in_place");
  });

  // A server older than the worktree save gate does not reject the mode — it
  // drops execution_mode, answers 201, and the task then runs in the folder the
  // user asked to isolate. Nothing downstream catches that, so the option has
  // to be closed here, and the copy has to say what is actually wrong (#7113).
  it("blocks parallel mode when the server cannot honour it", () => {
    const onConfirm = vi.fn();
    renderDialog({ unavailableReason: "server_outdated", onConfirm });

    const option = worktreeOption();
    expect(option.hasAttribute("disabled")).toBe(true);
    const notice = screen.getByText(/Multica server is too old/i);
    expect(notice.textContent).toMatch(/Update the server/i);

    fireEvent.click(option);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onConfirm).toHaveBeenCalledWith("in_place");
  });

  // The client no longer predicts whether the machine can run the mode — the
  // server decides on save, and this is where its answer lands. Guessing it up
  // front is what disabled the option for a user whose machine was already on
  // the newest release, with an instruction that could not help (#7113).
  it("shows a server rejection inline so the dialog stays actionable", () => {
    renderDialog({
      errorMessage:
        "the Multica runtime on that machine does not support it. Update the Multica app on that machine",
    });
    expect(screen.getByText(/does not support it/i)).toBeTruthy();
  });

  it("leaves parallel mode selectable for a git folder, whatever the runtime says", () => {
    const onConfirm = vi.fn();
    renderDialog({ onConfirm });

    const option = worktreeOption();
    expect(option.hasAttribute("disabled")).toBe(false);
    fireEvent.click(option);
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(onConfirm).toHaveBeenCalledWith("worktree");
  });
});
