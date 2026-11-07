import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { CliInstallInstructions } from "./cli-install-instructions";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const WINDOWS_CMD =
  "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex";

const clipboard = vi.hoisted(() => ({ copyText: vi.fn() }));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: clipboard.copyText }));

function renderInstructions() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CliInstallInstructions />
    </I18nProvider>,
  );
}

function resetConfigStore() {
  configStore.setState({
    cdnDomain: "",
    allowSignup: true,
    googleClientId: "",
    daemonServerUrl: "",
    daemonAppUrl: "",
    daemonConfigStatus: "loading",
    daemonSetupTarget: "unknown",
    workspaceCreationDisabled: false,
  });
}

describe("CliInstallInstructions", () => {
  beforeEach(() => {
    resetConfigStore();
    clipboard.copyText.mockReset().mockResolvedValue(true);
  });
  afterEach(() => {
    cleanup();
    resetConfigStore();
  });

  it("does not offer a setup command before connection settings resolve", () => {
    renderInstructions();
    expect(screen.queryByText("multica setup")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Copy" })).toHaveLength(1);
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading connection settings",
    );
  });

  it("withholds setup after a failure, then copies only the late-arriving self-host command", async () => {
    const user = userEvent.setup();
    renderInstructions();
    act(() => configStore.getState().setDaemonConfigStatus("error"));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn't load connection settings",
    );
    expect(screen.queryByText("multica setup")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Copy" })).toHaveLength(1);
    act(() => configStore.getState().setDaemonConfig({
      daemonSetupTarget: "self_host",
      daemonServerUrl: "https://api.example.com",
      daemonAppUrl: "https://app.example.com",
    }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "Copy" })[1]!);
    expect(clipboard.copyText).toHaveBeenCalledExactlyOnceWith(
      "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
  });

  // The switch itself is covered in common/cli-install-command.test.tsx; this
  // checks the card wires it into step 1 and leaves step 2 shared.
  it("offers the Windows installer in step 1 and keeps setup shared", async () => {
    configStore.getState().setDaemonConfig({ daemonSetupTarget: "cloud" });
    const user = userEvent.setup();
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <CliInstallInstructions />
      </I18nProvider>,
    );

    await user.click(screen.getByRole("tab", { name: "Windows" }));

    expect(screen.getByText(WINDOWS_CMD)).toBeInTheDocument();
    expect(screen.getByText("multica setup")).toBeInTheDocument();
  });

  it("renders self-host setup command when daemon URLs are configured", () => {
    configStore.getState().setDaemonConfig({
      daemonSetupTarget: "self_host",
      daemonServerUrl: "https://api.example.com/",
      daemonAppUrl: "https://app.example.com/",
    });

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <CliInstallInstructions />
      </I18nProvider>,
    );

    expect(
      screen.getByText(
        "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText("multica setup")).not.toBeInTheDocument();
  });
});
