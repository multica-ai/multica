import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";

vi.mock("@multica/i18n/react", async () => {
  const { en } = await import("@multica/i18n/dict/en");
  return {
    useT: (ns?: string) => (key: string, params?: Record<string, string | number>) => {
      const template = ns ? en[ns]?.[key] ?? key : key;
      if (!params) return template;
      return template.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? `{${k}}`));
    },
  };
});

import { StepQuestionnaire } from "./step-questionnaire";

const EMPTY_ANSWERS: QuestionnaireAnswers = {
  team_size: null,
  team_size_other: null,
  role: null,
  role_other: null,
  use_case: null,
  use_case_other: null,
};

function renderStep(initial: Partial<QuestionnaireAnswers> = {}) {
  const onSubmit = vi.fn();
  render(
    <StepQuestionnaire
      initial={{ ...EMPTY_ANSWERS, ...initial }}
      onSubmit={onSubmit}
    />,
  );
  return { onSubmit };
}

/**
 * The Continue button is the product of a strict "all three answered"
 * gate — no Skip path. These tests lock down that policy so a future
 * refactor can't silently loosen it:
 *   - Disabled by default (zero answers)
 *   - Stays disabled with 1 or 2 answers
 *   - Enabled only when all three have concrete selections
 *   - Stays disabled when any "Other" selection has empty text
 *   - Clicking while disabled never calls onSubmit
 *   - Switching away from Other clears that question's *_other field
 */
describe("StepQuestionnaire", () => {
  it("Continue is disabled when no questions are answered", () => {
    renderStep();
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeDisabled();
  });

  it("Continue stays disabled with only one question answered", async () => {
    const user = userEvent.setup();
    renderStep();
    await user.click(screen.getByRole("radio", { name: /just me/i }));
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeDisabled();
  });

  it("Continue stays disabled with only two questions answered", async () => {
    const user = userEvent.setup();
    renderStep();
    await user.click(screen.getByRole("radio", { name: /just me/i }));
    await user.click(
      screen.getByRole("radio", { name: /software developer/i }),
    );
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeDisabled();
  });

  it("Continue enables when all three questions are answered", async () => {
    const user = userEvent.setup();
    renderStep();
    await user.click(screen.getByRole("radio", { name: /just me/i }));
    await user.click(
      screen.getByRole("radio", { name: /software developer/i }),
    );
    await user.click(
      screen.getByRole("radio", { name: /write and ship code/i }),
    );
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeEnabled();
  });

  it("Continue stays disabled when Other is picked but its text is empty", async () => {
    const user = userEvent.setup();
    renderStep({
      team_size: "solo",
      role: "developer",
      // Q3 will be set to Other in-test — no text typed yet.
    });
    const q3Other = screen.getAllByRole("radio", { name: /^other$/i })[2]!;
    await user.click(q3Other);
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeDisabled();
  });

  it("Continue re-enables once Other text is filled in", async () => {
    const user = userEvent.setup();
    renderStep({ team_size: "solo", role: "developer" });
    const q3Other = screen.getAllByRole("radio", { name: /^other$/i })[2]!;
    await user.click(q3Other);
    const input = screen.getByPlaceholderText(/automate my weekly reports/i);
    await user.type(input, "Teach me the system");
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeEnabled();
  });

  it("clears Other text when the user switches to a concrete option", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderStep({
      role: "developer",
      use_case: "coding",
    });

    // Pick Q1 Other → type → switch to Just me → submit.
    // Submitted payload must have team_size_other = null.
    const q1Other = screen.getAllByRole("radio", { name: /^other$/i })[0]!;
    await user.click(q1Other);
    await user.type(
      screen.getByPlaceholderText(/small community i help run/i),
      "large enterprise",
    );
    await user.click(screen.getByRole("radio", { name: /just me/i }));
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        team_size: "solo",
        team_size_other: null,
      }),
    );
  });

  it("does not call onSubmit when Continue is disabled", async () => {
    // Belt-and-suspenders against a future refactor that replaces
    // <Button disabled> with a custom element that doesn't honor
    // the native disabled semantics — the handler's own
    // canContinue short-circuit catches it either way.
    const user = userEvent.setup();
    const { onSubmit } = renderStep();
    await user.click(screen.getByRole("button", { name: /continue/i }));
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("respects the initial prop (used for resume-after-back)", () => {
    renderStep({ team_size: "team", role: "developer" });
    expect(
      screen.getByRole("radio", { name: /my team/i }),
    ).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByRole("radio", { name: /software developer/i }),
    ).toHaveAttribute("aria-checked", "true");
  });

  it("submits the full answer set including all three questions", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderStep();

    await user.click(screen.getByRole("radio", { name: /just me/i }));
    await user.click(
      screen.getByRole("radio", { name: /software developer/i }),
    );
    await user.click(
      screen.getByRole("radio", { name: /write and ship code/i }),
    );
    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      team_size: "solo",
      team_size_other: null,
      role: "developer",
      role_other: null,
      use_case: "coding",
      use_case_other: null,
    });
  });
});
