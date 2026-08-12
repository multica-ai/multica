import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocalStackOverlay } from "./local-stack-overlay";

// This repo has no @testing-library/user-event dependency — interaction tests
// use fireEvent (see src/renderer/src/components/route-error-page.test.tsx).

describe("LocalStackOverlay", () => {
  it("shows the current step while running", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "running", step: "engine" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByText(/starting docker engine/i)).toBeInTheDocument();
  });

  it("marks earlier steps as done", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "running", step: "backend" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-engine")).toHaveAttribute(
      "data-status",
      "done",
    );
    expect(screen.getByTestId("step-backend")).toHaveAttribute(
      "data-status",
      "active",
    );
  });

  it("shows the failure message and both actions on failure", () => {
    const onRetry = vi.fn();
    const onSkip = vi.fn();
    render(
      <LocalStackOverlay
        state={{ phase: "failed", step: "containers", message: "compose exploded" }}
        onRetry={onRetry}
        onSkip={onSkip}
      />,
    );

    expect(screen.getByText("compose exploded")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(onRetry).toHaveBeenCalledOnce();
    fireEvent.click(screen.getByRole("button", { name: /continue anyway/i }));
    expect(onSkip).toHaveBeenCalledOnce();
  });

  it("marks the failed step so the user sees where it stopped", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "failed", step: "containers", message: "boom" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-containers")).toHaveAttribute(
      "data-status",
      "failed",
    );
  });

  // A config-load failure happens before any bring-up step runs. It must land
  // on the config row, not on probe — misattributing it sends the user
  // debugging docker when the real problem is a typo in their JSON.
  it("attributes a config failure to the config step, not probe", () => {
    render(
      <LocalStackOverlay
        state={{
          phase: "failed",
          step: "config",
          message: "local stack config: repoDir is required",
        }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-config")).toHaveAttribute(
      "data-status",
      "failed",
    );
    expect(screen.getByTestId("step-probe")).toHaveAttribute(
      "data-status",
      "pending",
    );
  });

  // The happy path never emits a running:config event — config loading is
  // instantaneous — so by the time probe is active, config must already read
  // as done rather than being stuck pending.
  it("shows config as done once probe is active", () => {
    render(
      <LocalStackOverlay
        state={{ phase: "running", step: "probe" }}
        onRetry={() => {}}
        onSkip={() => {}}
      />,
    );
    expect(screen.getByTestId("step-config")).toHaveAttribute(
      "data-status",
      "done",
    );
  });
});
