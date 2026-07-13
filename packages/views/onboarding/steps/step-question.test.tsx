import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { StepQuestion, type QuestionOption } from "./step-question";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const OPTIONS: readonly QuestionOption[] = [
  { slug: "a", icon: <span>A</span>, label: "Alpha" },
  { slug: "b", icon: <span>B</span>, label: "Beta" },
  { slug: "other", icon: <span>O</span>, label: "Other", isOther: true },
];

function renderShell(overrides: Partial<React.ComponentProps<typeof StepQuestion>> = {}) {
  const onAnswer = vi.fn();
  const onAdvance = vi.fn();
  const onSkip = vi.fn();
  const onBack = vi.fn();
  const onOtherChange = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepQuestion
        step="source"
        number={1}
        question="Test question"
        options={OPTIONS}
        selectedSlugs={[]}
        otherValue=""
        onOtherChange={onOtherChange}
        otherPlaceholder="type here"
        onAnswer={onAnswer}
        onAdvance={onAdvance}
        onSkip={onSkip}
        onBack={onBack}
        {...overrides}
      />
    </I18nProvider>,
  );
  return { onAnswer, onAdvance, onSkip, onBack, onOtherChange };
}

describe("StepQuestion", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("Continue is disabled when nothing is selected", () => {
    renderShell();
    const continueBtn = screen.getByRole("button", { name: /continue/i });
    expect(continueBtn).toBeDisabled();
  });

  it("non-Other option: clicking records the slug but does not auto-advance", async () => {
    const user = userEvent.setup();
    const { onAnswer, onAdvance } = renderShell();
    await user.click(screen.getByRole("radio", { name: /alpha/i }));
    expect(onAnswer).toHaveBeenCalledWith("a");
    expect(onAdvance).not.toHaveBeenCalled();
  });

  it("with a non-Other slug selected, Continue is enabled and fires onAdvance", async () => {
    const user = userEvent.setup();
    const { onAdvance } = renderShell({ selectedSlugs: ["a"] });
    const continueBtn = screen.getByRole("button", { name: /continue/i });
    expect(continueBtn).toBeEnabled();
    await user.click(continueBtn);
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it("Other selected with empty input → Continue stays disabled", async () => {
    const user = userEvent.setup();
    renderShell();
    await user.click(screen.getByRole("radio", { name: /^other$/i }));
    // pendingOther is now true; Continue must remain disabled until the
    // free-text input has content.
    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
  });

  it("Other selected with non-blank otherValue → Continue is enabled", async () => {
    const user = userEvent.setup();
    const { onAdvance } = renderShell({
      selectedSlugs: ["other"],
      otherValue: "hello",
    });
    const continueBtn = screen.getByRole("button", { name: /continue/i });
    expect(continueBtn).toBeEnabled();
    await user.click(continueBtn);
    expect(onAdvance).toHaveBeenCalledTimes(1);
  });

  it("Other selected with whitespace-only otherValue → Continue is disabled", () => {
    renderShell({ selectedSlugs: ["other"], otherValue: "   " });
    expect(screen.getByRole("button", { name: /continue/i })).toBeDisabled();
  });

  it("Skip is always enabled and fires onSkip", async () => {
    const user = userEvent.setup();
    const { onSkip } = renderShell();
    await user.click(screen.getByRole("button", { name: /^skip$/i }));
    expect(onSkip).toHaveBeenCalledTimes(1);
  });

  it("Back button is rendered only when onBack is provided", () => {
    const { unmount } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepQuestion
          step="source"
          number={1}
          question="Test"
          options={OPTIONS}
          selectedSlugs={[]}
          otherValue=""
          onOtherChange={vi.fn()}
          otherPlaceholder="type"
          onAnswer={vi.fn()}
          onAdvance={vi.fn()}
          onSkip={vi.fn()}
        />
      </I18nProvider>,
    );
    expect(screen.queryByRole("button", { name: /^back$/i })).not.toBeInTheDocument();
    unmount();
  });
});
