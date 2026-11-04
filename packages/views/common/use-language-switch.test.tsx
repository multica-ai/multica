import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { SUPPORTED_LOCALES } from "@multica/core/i18n";
import settings from "../locales/ru/settings.json";
import { useLanguageSwitch } from "./use-language-switch";

const state = vi.hoisted(() => ({
  language: "ru",
  persist: vi.fn(),
  updateMe: vi.fn(),
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (selector: (resource: typeof settings) => string) => selector(settings),
    i18n: { language: state.language },
  }),
}));

vi.mock("@multica/core/i18n/react", () => ({
  useLocaleAdapter: () => ({ persist: state.persist }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (store: { user: null }) => unknown) =>
    selector({ user: null }),
}));

vi.mock("@multica/core/api", () => ({
  api: { updateMe: state.updateMe },
}));

beforeEach(() => {
  state.language = "ru";
  vi.clearAllMocks();
});

describe("shared language switch", () => {
  it("offers every supported locale in Settings and the account menu", () => {
    const { result } = renderHook(() => useLanguageSwitch());

    expect(result.current.options.map(({ value }) => value)).toEqual(
      [...SUPPORTED_LOCALES],
    );
    expect(result.current.options.find(({ value }) => value === "ru")).toEqual({
      value: "ru",
      label: "Русский",
    });
  });

  it.each(["ru", "ru-RU"])(
    "recognizes %s and avoids persisting an unchanged language",
    async (language) => {
      state.language = language;
      const { result } = renderHook(() => useLanguageSwitch());

      expect(result.current.currentLocale).toBe("ru");
      await result.current.changeLanguage("ru");
      expect(state.persist).not.toHaveBeenCalled();
      expect(state.updateMe).not.toHaveBeenCalled();
    },
  );
});
