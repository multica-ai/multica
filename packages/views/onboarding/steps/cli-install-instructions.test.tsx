import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enOnboarding from "../../locales/en/onboarding.json";
import { CliInstallInstructions } from "./cli-install-instructions";

const TEST_RESOURCES = { en: { common: enCommon, onboarding: enOnboarding } };

const WINDOWS_CMD =
  "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex";

describe("CliInstallInstructions", () => {
  // The switch itself is covered in common/cli-install-command.test.tsx; this
  // checks the card wires it into step 1 and leaves step 2 shared.
  it("offers the Windows installer in step 1 and keeps setup shared", async () => {
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
});
