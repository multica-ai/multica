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
    currentVersion?: string;
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
        currentVersion={overrides.currentVersion}
        minVersion="0.4.24"
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

  // The server refuses the save below the version floor; saying so up front
  // beats a bare 422 after the user has already committed to the choice.
  it("disables parallel mode for an outdated daemon and names both versions", () => {
    renderDialog({
      unavailableReason: "daemon_outdated",
      currentVersion: "0.4.10",
    });

    expect(worktreeOption().hasAttribute("disabled")).toBe(true);
    const notice = screen.getByText(/0\.4\.10/);
    expect(notice.textContent).toContain("0.4.24");
  });

  it("falls back to a readable phrase when the daemon reports no version", () => {
    renderDialog({ unavailableReason: "daemon_outdated", currentVersion: "" });
    expect(screen.getByText(/an older version/i)).toBeTruthy();
  });

  it("shows a server rejection inline so the dialog stays actionable", () => {
    renderDialog({ errorMessage: "daemon is too old to run worktree mode" });
    expect(screen.getByText(/too old to run worktree mode/i)).toBeTruthy();
  });
});
