import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import {
  StepHeading,
  StepShell,
} from "./step-shell";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

function renderShell(props: Partial<Parameters<typeof StepShell>[0]> = {}) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepShell currentStep="workspace" {...props}>
        <div>step content</div>
      </StepShell>
    </I18nProvider>,
  );
}

describe("onboarding step shell", () => {

  // The panes persist across steps, so a screen reader user gets no
  // navigation event when the step changes — the heading has to announce.
  it("announces the step heading and renders it as the page h1", () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <StepHeading title="Name your workspace" description="Pick a URL." />
      </I18nProvider>,
    );

    const heading = screen.getByRole("heading", { name: "Name your workspace" });
    expect(heading.tagName).toBe("H1");
    expect(heading.closest("[aria-live]")).not.toBeNull();
    expect(screen.getByText("Pick a URL.")).toBeInTheDocument();
  });

  // Two Back buttons is the intended shape, not a duplicate: the rail is
  // hidden below `md` and the compact bar above the content is hidden at and
  // above it. jsdom applies no media queries, so both are in the tree here.
  it("renders Back only when the step can go back", () => {
    const { unmount } = renderShell();
    expect(screen.queryAllByRole("button", { name: /back/i })).toHaveLength(0);
    unmount();

    renderShell({ onBack: () => {} });
    expect(screen.getAllByRole("button", { name: /back/i })).toHaveLength(2);
  });

  it("disables Back while the step reports work in flight", () => {
    renderShell({ onBack: () => {}, backDisabled: true });
    for (const back of screen.getAllByRole("button", { name: /back/i })) {
      expect(back).toBeDisabled();
    }
  });

  // The rail does not fit under `md` — it never went below 15rem while the
  // content pane kept its gutter, which left ~87px of form at 375px. Hiding it
  // is only safe because the compact bar carries the same jobs.
  it("keeps a compact progress bar for widths where the rail is hidden", () => {
    const { container } = renderShell({
      currentStep: "runtime",
      onBack: () => {},
    });

    expect(container.querySelector("aside")!.className).toContain("hidden");

    const compact = container.querySelector("main .md\\:hidden")!;
    expect(compact).not.toBeNull();
    expect(compact.textContent).toContain("Meet Mika");
    expect(compact.querySelector("button")).not.toBeNull();
  });

  // The escape hatch lived only in the rail's footer, so hiding the rail left
  // every step but Welcome with no way to log out on a narrow screen.
  it("renders the escape hatch in whichever chrome is visible", () => {
    const { container } = renderShell({
      currentStep: "runtime",
      chromeFooter: <button type="button">Log out</button>,
    });

    const rail = within(container.querySelector("aside")!);
    expect(rail.getByRole("button", { name: /log out/i })).toBeInTheDocument();

    const compact = within(
      container.querySelector("main .md\\:hidden") as HTMLElement,
    );
    expect(
      compact.getByRole("button", { name: /log out/i }),
    ).toBeInTheDocument();
  });
});

describe("onboarding progress rail", () => {

  it("marks the current step for assistive tech", () => {
    const { container } = renderShell({ currentStep: "workspace" });

    const current = container.querySelector('[aria-current="step"]')!;
    expect(current).toBeInTheDocument();
    expect(current.textContent).toContain("Workspace");
  });

  // Forward navigation has to run the current step's validation and submit, so
  // only the steps already behind the member are reachable from the rail.
  it("links completed steps and leaves the current and later ones inert", async () => {
    const onStepChange = vi.fn();
    renderShell({ currentStep: "workspace", onStepChange });

    const back = screen.getByRole("button", { name: /about you/i });
    await userEvent.click(back);
    expect(onStepChange).toHaveBeenCalledWith("about_you");

    expect(screen.queryByRole("button", { name: /meet mika/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /^workspace/i })).toBeNull();
  });

  it("is display-only when the flow supplies no step handler", () => {
    renderShell({ currentStep: "runtime" });

    expect(screen.queryByRole("button", { name: /about you/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /^workspace/i })).toBeNull();
  });

  // Back is disabled precisely while a step has a request in flight; letting
  // the rail jump away would abandon it mid-create.
  it("stops rail navigation while the step reports work in flight", () => {
    renderShell({
      currentStep: "workspace",
      onStepChange: vi.fn(),
      backDisabled: true,
    });

    expect(screen.queryByRole("button", { name: /about you/i })).toBeNull();
  });
});
