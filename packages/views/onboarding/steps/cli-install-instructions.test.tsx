import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { CliInstallInstructions } from "./cli-install-instructions";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const ligatureClasses = [
  "[font-variant-ligatures:none]",
  "[font-feature-settings:'liga'_0]",
];

function resetConfigStore() {
  configStore.setState({ daemonServerUrl: "", daemonAppUrl: "" });
}

function renderInstructions() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CliInstallInstructions />
    </I18nProvider>,
  );
}

describe("CliInstallInstructions", () => {
  beforeEach(() => {
    resetConfigStore();
  });

  it("disables font ligatures in CLI command code", () => {
    renderInstructions();

    expect(screen.getByText("multica setup")).toHaveClass(...ligatureClasses);
  });

  it("falls back to the cloud setup command when no daemon URLs are configured", () => {
    renderInstructions();

    expect(screen.getByText("multica setup")).toBeInTheDocument();
    expect(
      screen.queryByText(/multica setup self-host/),
    ).not.toBeInTheDocument();
  });

  it("renders the self-host setup command derived from the runtime config", () => {
    configStore.getState().setDaemonConfig({
      daemonServerUrl: "https://multica-api.huya.info/",
      daemonAppUrl: "https://multica.huya.info/",
    });

    renderInstructions();

    expect(
      screen.getByText(
        "multica setup self-host --server-url https://multica-api.huya.info --app-url https://multica.huya.info",
      ),
    ).toBeInTheDocument();
  });
});
