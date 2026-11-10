import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { StepWelcome } from "./step-welcome";

describe("StepWelcome", () => {
  it("names the coding tools the desktop daemon found", () => {
    renderWithI18n(
      <StepWelcome onStart={vi.fn()} detectedProviders={["claude", "codex"]} />,
    );

    expect(
      screen.getByText("Found Claude Code and Codex on this computer."),
    ).toBeInTheDocument();
  });

  it("lists the familiar tools first, whatever order the daemon reports", () => {
    renderWithI18n(
      <StepWelcome onStart={vi.fn()} detectedProviders={["opencode", "codex", "claude"]} />,
    );

    expect(
      screen.getByText("Found Claude Code, Codex, and OpenCode on this computer."),
    ).toBeInTheDocument();
  });

  it("keeps looking while the daemon is still starting, instead of reporting none", () => {
    renderWithI18n(<StepWelcome onStart={vi.fn()} detectedProviders={[]} detecting />);

    expect(screen.getByText("Looking for coding tools on this computer…")).toBeInTheDocument();
    expect(screen.queryByText(/No coding tools found/)).not.toBeInTheDocument();
  });

  it("says when nothing was found, so the next step's install rows are expected", () => {
    renderWithI18n(<StepWelcome onStart={vi.fn()} detectedProviders={[]} />);

    expect(
      screen.getByText(/No coding tools found on this computer yet/),
    ).toBeInTheDocument();
  });

  it("does not talk about this computer on web", () => {
    renderWithI18n(<StepWelcome onStart={vi.fn()} isWeb />);

    expect(screen.queryByText(/on this computer/)).not.toBeInTheDocument();
    expect(screen.getByText("Connect a computer")).toBeInTheDocument();
  });

  it("offers the existing-user skip only when given one", async () => {
    const onSkip = vi.fn().mockResolvedValue(undefined);
    const { rerender } = renderWithI18n(<StepWelcome onStart={vi.fn()} />);
    expect(
      screen.queryByRole("button", { name: "I've used Multica before" }),
    ).not.toBeInTheDocument();

    rerender(<StepWelcome onStart={vi.fn()} onSkip={onSkip} />);
    await userEvent.click(screen.getByRole("button", { name: "I've used Multica before" }));
    expect(onSkip).toHaveBeenCalledOnce();
  });
});
