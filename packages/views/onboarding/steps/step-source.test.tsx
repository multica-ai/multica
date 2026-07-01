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

describe("StepSource (single-select primary source)", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("clicking a non-Other option writes a one-element source array", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep();

    await user.click(screen.getByRole("radio", { name: /linkedin/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["social_linkedin"],
      source_other: null,
      source_skipped: false,
    });
    // A click only records — it must NOT auto-advance.
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it("picking a second option replaces the first (no stacking)", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep({
      ...EMPTY,
      source: ["social_linkedin"],
    });

    await user.click(screen.getByRole("radio", { name: /twitter/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["social_x"],
      source_other: null,
      source_skipped: false,
    });
  });

  it("Skip clears source + source_other and marks the step skipped, then calls onSkip", async () => {
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

  it("Other: clicking writes `source: ['other']` and lets the user type into source_other", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep();

    await user.click(screen.getByRole("radio", { name: /^other$/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["other"],
      source_other: null,
      source_skipped: false,
    });

    const input = await screen.findByPlaceholderText(/podcast/i);
    await user.type(input, "x");
    expect(onChange).toHaveBeenLastCalledWith({ source_other: "x" });
  });

  it("switching away from Other clears source_other so a stale value can't leak", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep({
      ...EMPTY,
      source: ["other"],
      source_other: "a podcast",
    });

    await user.click(screen.getByRole("radio", { name: /linkedin/i }));

    expect(onChange).toHaveBeenCalledWith({
      source: ["social_linkedin"],
      source_other: null,
      source_skipped: false,
    });
  });
});
