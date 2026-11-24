// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { LocalDirectoryExecutionMode } from "@multica/core/types";
import enProjects from "../../locales/en/projects.json";
import enCommon from "../../locales/en/common.json";
import {
  LocalDirectoryModeOptions,
  type WorktreeUnavailableReason,
} from "./local-directory-mode-options";

const TEST_RESOURCES = { en: { projects: enProjects, common: enCommon } };

function renderOptions(
  overrides: {
    value?: LocalDirectoryExecutionMode;
    unavailableReason?: WorktreeUnavailableReason;
  } = {},
) {
  const onChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <LocalDirectoryModeOptions
        value={overrides.value ?? "in_place"}
        onChange={onChange}
        unavailableReason={overrides.unavailableReason}
      />
    </I18nProvider>,
  );
  return { onChange };
}

function worktreeOption(): HTMLElement {
  return screen.getAllByRole("radio")[1] as HTMLElement;
}

describe("LocalDirectoryModeOptions", () => {
  it("marks the current mode as selected", () => {
    renderOptions({ value: "worktree" });
    expect(worktreeOption().getAttribute("aria-checked")).toBe("true");
    expect(screen.getAllByRole("radio")[0]?.getAttribute("aria-checked")).toBe("false");
  });

  // A non-git folder cannot produce a branch, so offering the option would
  // guarantee the user's first task fails. Disable it where they choose.
  it("disables parallel mode for a non-git folder and says why", () => {
    const { onChange } = renderOptions({ unavailableReason: "not_git" });

    const option = worktreeOption();
    expect(option.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/not a git repository/i)).toBeTruthy();
    fireEvent.click(option);
    expect(onChange).not.toHaveBeenCalled();
  });

  // A server older than the worktree save gate does not reject the mode — it
  // drops execution_mode, answers 201, and the task then runs in the folder the
  // user asked to isolate. Nothing downstream catches that, so the option has
  // to be closed here, and the copy has to say what is actually wrong (#7113).
  it("blocks parallel mode when the server cannot honour it", () => {
    const { onChange } = renderOptions({ unavailableReason: "server_outdated" });

    const option = worktreeOption();
    expect(option.hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(/Multica server is too old/i).textContent).toMatch(
      /Update the server/i,
    );
    fireEvent.click(option);
    expect(onChange).not.toHaveBeenCalled();
  });

  // The client no longer predicts whether the machine can run the mode — the
  // server decides on save. Guessing it up front is what disabled the option
  // for a user whose machine was already on the newest release (#7113).
  it("leaves parallel mode selectable for a git folder, whatever the runtime says", () => {
    const { onChange } = renderOptions();

    const option = worktreeOption();
    expect(option.hasAttribute("disabled")).toBe(false);
    fireEvent.click(option);
    expect(onChange).toHaveBeenCalledWith("worktree");
  });
});
