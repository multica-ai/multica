// @vitest-environment jsdom

import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useTranslation } from "react-i18next";
import { I18nProvider } from "./provider";
import type { LocaleResources } from "./types";

afterEach(cleanup);

function DialogTitle() {
  const { t } = useTranslation("settings");
  return <span>{t("shortcuts.reset_confirm.title")}</span>;
}

describe("I18nProvider", () => {
  it("rebuilds its i18n instance when resources change", async () => {
    const initialResources: Record<string, LocaleResources> = {
      en: { settings: { shortcuts: { title: "Keyboard Shortcuts" } } },
    };
    const updatedResources: Record<string, LocaleResources> = {
      en: {
        settings: {
          shortcuts: {
            title: "Keyboard Shortcuts",
            reset_confirm: { title: "Restore all shortcut defaults?" },
          },
        },
      },
    };

    const { rerender } = render(
      <I18nProvider locale="en" resources={initialResources}>
        <DialogTitle />
      </I18nProvider>,
    );
    expect(screen.queryByText("shortcuts.reset_confirm.title")).not.toBeNull();

    rerender(
      <I18nProvider locale="en" resources={updatedResources}>
        <DialogTitle />
      </I18nProvider>,
    );
    await waitFor(() => {
      expect(
        screen.queryByText("Restore all shortcut defaults?"),
      ).not.toBeNull();
    });
  });
});
