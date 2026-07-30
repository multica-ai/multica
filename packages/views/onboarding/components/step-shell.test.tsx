import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import {
  STEP_BLOCK_PADDING,
  STEP_COLUMN,
  STEP_FRAME,
  STEP_GUTTER,
  STEP_MEASURE,
  StepShellHeader,
} from "./step-shell";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

function renderHeader(props: Parameters<typeof StepShellHeader>[0]) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepShellHeader {...props} />
    </I18nProvider>,
  );
}

describe("onboarding step shell", () => {
  // The regression this guards: the header used to carry the horizontal
  // padding itself, so it sat flush to the window edge while the content
  // column was centred. A 480px rail hid the difference; once the rail was
  // removed the two were 267px apart on a 1283px window, and the step
  // indicator floated off at the far right of the screen.
  it("pads the header from the gutter and measures its contents on the frame", () => {
    const { container } = renderHeader({ currentStep: "runtime" });

    const header = container.querySelector("header")!;
    for (const cls of STEP_GUTTER.split(" ")) {
      expect(header.className).toContain(cls);
    }
    expect(header.className).not.toMatch(/\bmax-w-/);

    const inner = header.firstElementChild!;
    for (const cls of STEP_FRAME.split(" ")) {
      expect(inner.className).toContain(cls);
    }
  });

  // Both measures centre, so a step that needs the wider one for its content
  // still sits on the header's centreline instead of shifting the page.
  it("centres both measures so content stays on the header's centreline", () => {
    for (const measure of [STEP_FRAME, STEP_COLUMN]) {
      expect(measure).toContain("mx-auto");
      expect(measure).toMatch(/max-w-\[\d+px\]/);
      // Padding belongs to the gutter. Inside a max-w box it would resolve to
      // a different reading width per breakpoint — the content jumped from
      // 508px to 620px at `lg` for exactly that reason.
      expect(measure).not.toMatch(/\bp[xlr]?-/);
    }
    expect(STEP_BLOCK_PADDING).toMatch(/^py-/);
  });

  // STEP_MEASURE caps prose and form fields inside a step that sits on the
  // frame. It must NOT centre: centring it would pull the content off the
  // frame's left edge, which is the alignment the frame exists to provide.
  it("keeps the in-frame reading measure left-aligned and padding-free", () => {
    expect(STEP_MEASURE).toMatch(/max-w-\[\d+px\]/);
    expect(STEP_MEASURE).not.toContain("mx-auto");
    expect(STEP_MEASURE).not.toMatch(/\bp[xlr]?-/);
  });

  it("renders Back only when the step can go back", () => {
    const { unmount } = renderHeader({ currentStep: "workspace" });
    expect(screen.queryByRole("button", { name: /back/i })).toBeNull();
    unmount();

    renderHeader({ currentStep: "workspace", onBack: () => {} });
    expect(screen.getByRole("button", { name: /back/i })).toBeInTheDocument();
  });

  it("disables Back while the step reports work in flight", () => {
    renderHeader({
      currentStep: "workspace",
      onBack: () => {},
      backDisabled: true,
    });
    expect(screen.getByRole("button", { name: /back/i })).toBeDisabled();
  });
});
