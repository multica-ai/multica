import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { StepSource } from "./step-source";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const EMPTY: QuestionnaireAnswers = {
  source: [],
  source_other: null,
  source_skipped: false,
  role: null,
  role_other: null,
  role_skipped: false,
  use_case: [],
  use_case_other: null,
  use_case_skipped: false,
  version: 2,
};

function renderStep(answers: QuestionnaireAnswers = EMPTY) {
  const onChange = vi.fn();
  const onAdvance = vi.fn();
  const onSkip = vi.fn();
  const onBack = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepSource
        answers={answers}
        onChange={onChange}
        onAdvance={onAdvance}
        onSkip={onSkip}
        onBack={onBack}
      />
    </I18nProvider>,
  );
  return { onChange, onAdvance, onSkip, onBack };
}

describe("StepSource (multi-select)", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("clicking a non-Other option appends the slug to the array", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep();

    await user.click(screen.getByRole("checkbox", { name: /linkedin/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["social_linkedin"],
      source_skipped: false,
    });
    // A click only records — it must NOT auto-advance.
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it("clicking an already-selected option removes it (toggle)", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep({
      ...EMPTY,
      source: ["social_linkedin"],
    });

    await user.click(screen.getByRole("checkbox", { name: /linkedin/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: [],
      source_skipped: false,
    });
  });

  it("multi-select stacks several picks", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep({
      ...EMPTY,
      source: ["social_linkedin"],
    });

    await user.click(screen.getByRole("checkbox", { name: /twitter/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["social_linkedin", "social_x"],
      source_skipped: false,
    });
  });

  it("Skip clears the array + other and marks the step skipped, then calls onSkip", async () => {
    const user = userEvent.setup();
    const { onChange, onSkip } = renderStep();

    await user.click(screen.getByRole("button", { name: /skip/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: [],
      source_other: null,
      source_skipped: true,
    });
    expect(onSkip).toHaveBeenCalledTimes(1);
  });

  it("Other: clicking adds 'other' to the array; free-text writes to source_other", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep();

    await user.click(screen.getByRole("checkbox", { name: /^other$/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["other"],
      source_skipped: false,
    });

    const input = await screen.findByPlaceholderText(/podcast/i);
    await user.type(input, "x");
    expect(onChange).toHaveBeenLastCalledWith({ source_other: "x" });
  });
});
