import { beforeEach, describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { useContentWidthStore } from "@multica/core/issues/stores/content-width-store";
import enSettings from "../../locales/en/settings.json";
import { ContentWidthRow } from "./preferences-tab";

beforeEach(() => {
  localStorage.clear();
  useContentWidthStore.setState({ contentWidth: "standard" });
});

describe("content width setting", () => {
  it("selects Full width immediately and persists the device choice", () => {
    render(
      <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
        <ContentWidthRow />
      </I18nProvider>,
    );
    const full = screen.getByRole("button", { name: "Full width" });
    expect(screen.getByRole("button", { name: "Standard" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(full);
    expect(full).toHaveAttribute("aria-pressed", "true");
    expect(useContentWidthStore.getState().contentWidth).toBe("full");
    expect(JSON.parse(localStorage.getItem("multica_content_width")!).state).toEqual({ contentWidth: "full" });
  });
});
