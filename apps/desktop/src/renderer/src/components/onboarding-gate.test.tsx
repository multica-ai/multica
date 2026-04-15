import { describe, it, expect } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { useState } from "react";
import { OnboardingGate } from "./onboarding-gate";

describe("OnboardingGate", () => {
  it("renders children when a workspace exists at mount", () => {
    render(
      <OnboardingGate
        hasWorkspace={true}
        onboarding={() => <div data-testid="wizard">wizard</div>}
      >
        <div data-testid="main">main shell</div>
      </OnboardingGate>,
    );

    expect(screen.getByTestId("main")).toBeInTheDocument();
    expect(screen.queryByTestId("wizard")).not.toBeInTheDocument();
  });

  it("keeps the wizard mounted even after hasWorkspace flips to true mid-flow", () => {
    // Controls the hasWorkspace prop from outside so we can simulate
    // step 0 of the wizard creating a workspace while steps 1-3 still need
    // to render. The gate should ignore the prop change and hold the wizard.
    function Harness() {
      const [hasWorkspace, setHasWorkspace] = useState(false);
      return (
        <>
          <button
            type="button"
            data-testid="grant-workspace"
            onClick={() => setHasWorkspace(true)}
          >
            grant
          </button>
          <OnboardingGate
            hasWorkspace={hasWorkspace}
            onboarding={() => <div data-testid="wizard">wizard</div>}
          >
            <div data-testid="main">main shell</div>
          </OnboardingGate>
        </>
      );
    }

    render(<Harness />);
    expect(screen.getByTestId("wizard")).toBeInTheDocument();

    act(() => {
      screen.getByTestId("grant-workspace").click();
    });

    // Prop change alone does not dismiss the wizard — only onComplete does.
    expect(screen.getByTestId("wizard")).toBeInTheDocument();
    expect(screen.queryByTestId("main")).not.toBeInTheDocument();
  });

  it("transitions to children after the wizard calls onComplete", () => {
    render(
      <OnboardingGate
        hasWorkspace={false}
        onboarding={(onComplete) => (
          <button type="button" data-testid="finish" onClick={onComplete}>
            finish
          </button>
        )}
      >
        <div data-testid="main">main shell</div>
      </OnboardingGate>,
    );

    expect(screen.getByTestId("finish")).toBeInTheDocument();

    act(() => {
      screen.getByTestId("finish").click();
    });

    expect(screen.getByTestId("main")).toBeInTheDocument();
    expect(screen.queryByTestId("finish")).not.toBeInTheDocument();
  });
});
