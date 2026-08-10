import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { CliInstallInstructions } from "./cli-install-instructions";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const ligatureClasses = [
  "[font-variant-ligatures:none]",
  "[font-feature-settings:'liga'_0]",
];

describe("CliInstallInstructions", () => {
  it("disables font ligatures in CLI command code", () => {
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <CliInstallInstructions />
      </I18nProvider>,
    );

    expect(screen.getByText("multica setup")).toHaveClass(...ligatureClasses);
  });

  it("switches between macOS/Linux and Windows installer commands", async () => {
    const user = userEvent.setup();
    const { baseElement } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <CliInstallInstructions />
      </I18nProvider>,
    );

    expect(screen.getByRole("button", { name: "macOS / Linux" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(baseElement).toHaveTextContent(
      "curl -fsSL https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.sh | bash",
    );

    await user.click(screen.getByRole("button", { name: "Windows" }));

    expect(baseElement).toHaveTextContent(
      "irm https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.ps1 | iex",
    );
    expect(baseElement).not.toHaveTextContent("curl -fsSL");
  });
});
