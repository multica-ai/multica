import { configStore } from "@multica/core/config";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  CLI_INSTALL_COMMANDS,
  CliInstallCommand,
} from "./cli-install-command";

const LABELS = {
  group: "Choose your platform",
  macosLinux: "macOS / Linux",
  windows: "Windows",
};

function renderSwitch() {
  return render(
    <CliInstallCommand labels={LABELS}>
      {(command) => <code>{command}</code>}
    </CliInstallCommand>,
  );
}

describe("CliInstallCommand", () => {
  it("defaults to the macOS / Linux installer", () => {
    renderSwitch();

    expect(
      screen.getByRole("tablist", { name: LABELS.group }),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "macOS / Linux" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(
      screen.getByText(CLI_INSTALL_COMMANDS.macosLinux),
    ).toBeInTheDocument();
    expect(screen.queryByText(CLI_INSTALL_COMMANDS.windows)).toBeNull();
  });

  it("hands the Windows command to the row when that tab is picked", async () => {
    const user = userEvent.setup();
    renderSwitch();

    await user.click(screen.getByRole("tab", { name: "Windows" }));

    expect(screen.getByRole("tab", { name: "Windows" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByText(CLI_INSTALL_COMMANDS.windows)).toBeInTheDocument();
    expect(screen.queryByText(CLI_INSTALL_COMMANDS.macosLinux)).toBeNull();
  });

  // A real tablist: arrow keys move between platforms without a click.
  it("switches platform with the arrow keys", async () => {
    const user = userEvent.setup();
    renderSwitch();

    await user.click(screen.getByRole("tab", { name: "macOS / Linux" }));
    await user.keyboard("{ArrowRight}");

    expect(screen.getByText(CLI_INSTALL_COMMANDS.windows)).toBeInTheDocument();
  });
});

afterEach(() => {
  configStore.setState({ cliInstallCommandUnix: "", cliInstallCommandWindows: "" });
});

it("shows deployment install commands for both platforms", async () => {
  configStore.setState({
    cliInstallCommandUnix: "bash ./internal-install.sh",
    cliInstallCommandWindows: "./internal-install.ps1",
  });
  const user = userEvent.setup();
  renderSwitch();
  expect(screen.getByText("bash ./internal-install.sh")).toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Windows" }));
  expect(screen.getByText("./internal-install.ps1")).toBeInTheDocument();
  expect(screen.queryByText(CLI_INSTALL_COMMANDS.windows)).toBeNull();
});

beforeEach(() => configStore.setState({ daemonConfigStatus: "ready" }));
it("withholds default and stale commands until deployment config is ready", () => {
  configStore.setState({daemonConfigStatus: "loading", cliInstallCommandUnix: "bash previous-server.sh"});
  const {container} = renderSwitch();
  expect(container.querySelector("code")).toBeNull();
  act(() => configStore.setState({daemonConfigStatus: "error"}));
  expect(container.querySelector("code")).toBeNull();
  act(() => configStore.setState({daemonConfigStatus: "ready", cliInstallCommandUnix: ""}));
  expect(screen.getByText(CLI_INSTALL_COMMANDS.macosLinux)).toBeInTheDocument();
});
