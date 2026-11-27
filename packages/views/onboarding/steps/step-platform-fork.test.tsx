import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const clipboard = vi.hoisted(() => ({ copyText: vi.fn() }));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: clipboard.copyText }));

import { StepPlatformFork } from "./step-platform-fork";

function renderFork() {
  const onSkip = vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <StepPlatformFork
        onSkip={onSkip}
        cliInstructions={<div data-testid="cli-instructions">install me</div>}
      />
    </I18nProvider>,
  );
  return { onSkip };
}

describe("StepPlatformFork", () => {
  beforeEach(() => {
    configStore.getState().setDaemonConfig({ daemonSetupTarget: "cloud" });
    clipboard.copyText.mockReset().mockResolvedValue(true);
    vi.restoreAllMocks();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    configStore.setState({
      daemonConfigStatus: "loading",
      daemonSetupTarget: "unknown",
      daemonServerUrl: "",
      daemonAppUrl: "",
    });
  });

  it("recommends Desktop and offers the terminal and another computer", () => {
    renderFork();
    expect(screen.getByText("Download Multica Desktop")).toBeInTheDocument();
    expect(screen.getByText("Connect from the terminal")).toBeInTheDocument();
    expect(screen.getByText("Continue on another computer")).toBeInTheDocument();
    expect(screen.getByText(/continues on its own once Desktop connects/i)).toBeInTheDocument();
    // Nothing about Mika, and no Continue: the flow moves on by itself.
    expect(screen.queryByText(/mika/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /^continue$/i })).toBeNull();
  });

  it("opens the download page and shows what happens next", async () => {
    const openSpy = vi.spyOn(window, "open").mockReturnValue(null);
    const user = userEvent.setup();
    renderFork();

    await user.click(screen.getByRole("button", { name: /^download$/i }));

    expect(openSpy).toHaveBeenCalledWith("/download", "_blank", "noopener,noreferrer");
    expect(screen.getByText("Open Multica Desktop")).toBeInTheDocument();
    expect(screen.getByText("Install an AI coding tool and create your agent")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /download again/i })).toBeInTheDocument();
  });

  it("copies a link that resumes onboarding on another computer", async () => {
    const user = userEvent.setup();
    renderFork();

    await user.click(screen.getByRole("button", { name: /copy link/i }));

    expect(clipboard.copyText).toHaveBeenCalledWith(`${window.location.origin}/onboarding`);
    expect(await screen.findByRole("button", { name: /link copied/i })).toBeInTheDocument();
  });

  it("skips with the guide hint next to the button", async () => {
    const user = userEvent.setup();
    const { onSkip } = renderFork();
    expect(screen.getByText(/we'll add a setup guide/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /skip for now/i }));
    expect(onSkip).toHaveBeenCalledTimes(1);
  });

  it("terminal dialog shows the instructions and listens, with no continue button of its own", async () => {
    const user = userEvent.setup();
    renderFork();
    await user.click(screen.getByRole("button", { name: /show steps/i }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByTestId("cli-instructions")).toBeInTheDocument();
    expect(within(dialog).getByText(/waiting for your computer/i)).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: /continue/i })).toBeNull();
  });

  it("uses the same self-host command throughout the waiting stages", () => {
    vi.useFakeTimers();
    configStore.getState().setDaemonConfig({
      daemonSetupTarget: "self_host",
      daemonServerUrl: "https://api.example.com",
      daemonAppUrl: "https://app.example.com",
    });
    renderFork();
    fireEvent.click(screen.getByRole("button", { name: /show steps/i }));
    const dialog = screen.getByRole("dialog");
    const command = "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com";
    expect(within(dialog).getByText(command)).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(15_000));
    expect(within(dialog).getByText(command)).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(30_000));
    expect(within(dialog).getByText(command)).toBeInTheDocument();
    expect(within(dialog).queryByText("multica setup")).not.toBeInTheDocument();
  });

  it("does not show Cloud waiting guidance while configuration is loading or failed", () => {
    configStore.getState().setDaemonConfigStatus("loading");
    renderFork();
    fireEvent.click(screen.getByRole("button", { name: /show steps/i }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).queryByText("multica setup")).not.toBeInTheDocument();
    expect(within(dialog).queryByText(/waiting for your computer/i)).not.toBeInTheDocument();
    act(() => configStore.getState().setDaemonConfigStatus("error"));
    expect(within(dialog).queryByText("multica setup")).not.toBeInTheDocument();
  });
});
