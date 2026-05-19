import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { StepUseCase } from "./step-use-case";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const EMPTY: QuestionnaireAnswers = {
  source: null,
  source_other: null,
  source_skipped: false,
  role: null,
  role_other: null,
  role_skipped: false,
  use_case: null,
  use_case_other: null,
  use_case_skipped: false,
  version: 2,
};

function renderStep(answers: QuestionnaireAnswers = EMPTY) {
  const onChange = vi.fn();
  const onAdvance = vi.fn();
  const onSkip = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepUseCase
        answers={answers}
        onChange={onChange}
        onAdvance={onAdvance}
        onSkip={onSkip}
      />
    </I18nProvider>,
  );
  return { onChange, onAdvance, onSkip };
}

describe("StepUseCase", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("selecting a use case patches the slug and clears Other/skip", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep();

    await user.click(screen.getByRole("radio", { name: /ship code/i }));

    expect(onChange).toHaveBeenCalledWith({
      use_case: "ship_code",
      use_case_other: null,
      use_case_skipped: false,
    });
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it("Skip clears slot and marks use_case_skipped", async () => {
    const user = userEvent.setup();
    const { onChange, onSkip } = renderStep();

    await user.click(screen.getByRole("button", { name: /skip/i }));

    expect(onChange).toHaveBeenCalledWith({
      use_case: null,
      use_case_other: null,
      use_case_skipped: true,
    });
    expect(onSkip).toHaveBeenCalledTimes(1);
  });

  it("Other: selecting + typing writes use_case_other through onChange", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep();

    await user.click(screen.getByRole("radio", { name: /^other$/i }));
    expect(onChange).toHaveBeenCalledWith({
      use_case: "other",
      use_case_skipped: false,
    });

    const input = await screen.findByPlaceholderText(/study group/i);
    await user.type(input, "z");
    expect(onChange).toHaveBeenLastCalledWith({ use_case_other: "z" });
  });
});
