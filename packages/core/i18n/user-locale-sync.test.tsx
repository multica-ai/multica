// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { useTranslation } from "react-i18next";
import {
  I18nProvider,
  LocaleAdapterProvider,
  UserLocaleSync,
} from "./react";
import type { LocaleAdapter, LocaleResources } from "./types";

const userLanguageRef = vi.hoisted(() => ({
  current: null as string | null,
}));

vi.mock("../auth", async () => {
  const actual = await vi.importActual<typeof import("../auth")>("../auth");
  const useAuthStore = (sel?: (state: { user: { language: string | null } | null }) => unknown) => {
    const state = userLanguageRef.current
      ? { user: { language: userLanguageRef.current } }
      : { user: null };
    return sel ? sel(state) : state;
  };
  return { ...actual, useAuthStore };
});

const resources: Record<string, LocaleResources> = {
  en: { common: { label: "English" } },
  "zh-Hans": { common: { label: "Chinese" } },
};

function Probe() {
  const { t } = useTranslation("common");
  return <div>{t("label")}</div>;
}

function renderSync(adapter: LocaleAdapter) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale="en" resources={resources}>
        <LocaleAdapterProvider adapter={adapter}>
          {children}
        </LocaleAdapterProvider>
      </I18nProvider>
    );
  }

  return render(
    <>
      <UserLocaleSync />
      <Probe />
    </>,
    { wrapper: Wrapper },
  );
}

describe("UserLocaleSync", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    userLanguageRef.current = null;
  });

  it("persists and applies a server-stored language without reloading", async () => {
    const reload = vi.fn();
    Object.defineProperty(window, "location", {
      writable: true,
      configurable: true,
      value: { reload },
    });
    userLanguageRef.current = "zh-Hans";
    const adapter: LocaleAdapter = {
      getUserChoice: () => null,
      getSystemPreferences: () => [],
      persist: vi.fn(),
    };

    renderSync(adapter);

    expect(adapter.persist).toHaveBeenCalledWith("zh-Hans");
    expect(reload).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(screen.getByText("Chinese")).toBeTruthy();
      expect(document.documentElement.lang).toBe("zh-CN");
    });
  });
});
