import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { StepAboutYou } from "./step-about-you";

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
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepAboutYou
        answers={answers}
        onChange={onChange}
        onAdvance={onAdvance}
        onSkip={onSkip}
      />
    </I18nProvider>,
  );
  return { onChange, onAdvance, onSkip };
}

describe("StepAboutYou", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders both question groups on one screen", () => {
    renderStep();
    expect(
      screen.getByText("Which best describes you?"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("What do you want to use Multica for?"),
    ).toBeInTheDocument();
  });

  it("selecting a role patches the slug and clears Other/skip", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep();

    await user.click(screen.getByRole("radio", { name: /engineer/i }));

    expect(onChange).toHaveBeenCalledWith({
      role: "engineer",
      role_other: null,
      role_skipped: false,
    });
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it("toggling a use case patches the array and clears skip", async () => {
    const user = userEvent.setup();
    const { onChange } = renderStep();

    await user.click(
      screen.getByRole("checkbox", { name: /ship code with ai agents/i }),
    );

    expect(onChange).toHaveBeenCalledWith({
      use_case: ["ship_code"],
      use_case_skipped: false,
    });
  });

  it("Continue is disabled until one group has a committed answer", async () => {
    const user = userEvent.setup();
    const { onAdvance } = renderStep();

    const cont = screen.getByRole("button", { name: /continue/i });
    expect(cont).toBeDisabled();
    await user.click(cont);
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it("Continue with only role answered marks use_case as skipped", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep({
      ...EMPTY,
      role: "engineer",
    });

    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(onChange).toHaveBeenCalledWith({
      use_case: [],
      use_case_other: null,
      use_case_skipped: true,
    });
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it("Continue with only use_case answered marks role as skipped", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep({
      ...EMPTY,
      use_case: ["ship_code"],
    });

    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(onChange).toHaveBeenCalledWith({
      role: null,
      role_other: null,
      role_skipped: true,
    });
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it("Continue with both groups answered patches nothing extra", async () => {
    const user = userEvent.setup();
    const { onChange, onAdvance } = renderStep({
      ...EMPTY,
      role: "engineer",
      use_case: ["ship_code"],
    });

    await user.click(screen.getByRole("button", { name: /continue/i }));

    expect(onChange).not.toHaveBeenCalled();
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it("a lone empty-text Other role does not enable Continue", () => {
    renderStep({ ...EMPTY, role: "other" });
    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
  });

  it("an Other role with text enables Continue", () => {
    renderStep({ ...EMPTY, role: "other", role_other: "teacher" });
    expect(
      screen.getByRole("button", { name: /continue/i }),
    ).toBeEnabled();
  });

  it("Skip declines both groups in one patch and advances", async () => {
    const user = userEvent.setup();
    const { onChange, onSkip } = renderStep();

    await user.click(screen.getByRole("button", { name: /skip/i }));

    expect(onChange).toHaveBeenCalledWith({
      role: null,
      role_other: null,
      role_skipped: true,
      use_case: [],
      use_case_other: null,
      use_case_skipped: true,
    });
    expect(onSkip).toHaveBeenCalledTimes(1);
  });

  it("pre-fills both groups from stored answers on re-entry", () => {
    renderStep({
      ...EMPTY,
      role: "designer",
      use_case: ["plan_research", "write_publish"],
    });

    expect(
      screen.getByRole("radio", { name: /designer/i }),
    ).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByRole("checkbox", { name: /plan, brainstorm, research/i }),
    ).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByRole("checkbox", { name: /write, edit, publish/i }),
    ).toHaveAttribute("aria-checked", "true");
  });
});
