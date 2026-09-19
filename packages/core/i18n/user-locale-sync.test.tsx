// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { LocaleAdapterProvider } from "./adapter-context";
import { I18nProvider } from "./provider";
import { UserLocaleSync } from "./user-locale-sync";
import type { SupportedLocale } from "./types";

vi.mock("../auth", () => ({
  useAuthStore: (selector: (state: { user: { language: string } }) => unknown) =>
    selector({ user: { language: "fr" } }),
}));

afterEach(cleanup);

it("restores saved French on another device and stops reloading once it matches", () => {
  const reload = vi.fn();
  const location = window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { reload },
  });
  const adapter = {
    getUserChoice: () => null,
    getSystemPreferences: () => ["en"],
    persist: vi.fn(),
  };
  const resources = {
    en: { common: { save: "Save" } },
    fr: { common: { save: "Enregistrer" } },
  };
  const tree = (locale: SupportedLocale) => (
    <I18nProvider locale={locale} resources={resources}>
      <LocaleAdapterProvider adapter={adapter}>
        <UserLocaleSync />
      </LocaleAdapterProvider>
    </I18nProvider>
  );

  try {
    const first = render(tree("en"));
    expect(adapter.persist).toHaveBeenCalledExactlyOnceWith("fr");
    expect(reload).toHaveBeenCalledTimes(1);
    first.unmount();

    adapter.persist.mockClear();
    reload.mockClear();
    render(tree("fr"));
    expect(adapter.persist).not.toHaveBeenCalled();
    expect(reload).not.toHaveBeenCalled();
  } finally {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: location,
    });
  }
});
