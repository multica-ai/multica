import { act, render, renderHook, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { useT } from "./use-t";

const resources = {
  en: { common: { daemon_setup: { loading: "Hello" }, time: { minutes_ago: "{{count}}m ago" } } },
  "zh-Hans": { common: { daemon_setup: { loading: "你好" }, time: { minutes_ago: "{{count}} 分钟前" } } },
};

function wrapper({ children }: { children: ReactNode }) {
  return <I18nProvider locale="en" resources={resources as never}>{children}</I18nProvider>;
}

function Greeting() {
  const { t } = useT("common");
  return <span>{t(($) => $.daemon_setup.loading)}</span>;
}

describe("useT", () => {
  it("translates and re-renders when the language changes", async () => {
    let i18n: ReturnType<typeof useT>["i18n"] | null = null;
    function Probe() {
      i18n = useT("common").i18n;
      return <Greeting />;
    }
    render(<Probe />, { wrapper });
    expect(screen.getByText("Hello")).toBeInTheDocument();
    const before = i18n;

    await act(async () => { await i18n!.changeLanguage("zh-Hans"); });

    expect(screen.getByText("你好")).toBeInTheDocument();
    // A new identity on a language change, so effects keyed on it rerun.
    expect(i18n).not.toBe(before);
    expect(i18n!.language).toBe("zh-Hans");
  });

  it("shares one t and i18n across components of a namespace", () => {
    const { result } = renderHook(() => [useT("common"), useT("common")] as const, { wrapper });
    const [a, b] = result.current;
    expect(a.t).toBe(b.t);
    expect(a.i18n).toBe(b.i18n);
    expect(a.ready).toBe(true);
    // Array form, as react-i18next returns.
    expect(a[0]).toBe(a.t);
  });

  it("answers repeated lookups the same and still applies options", async () => {
    const { result } = renderHook(() => useT("common"), { wrapper });
    const { t, i18n } = result.current;
    expect(t(($) => $.daemon_setup.loading)).toBe("Hello");
    expect(t(($) => $.daemon_setup.loading)).toBe("Hello");
    expect(t(($) => $.time.minutes_ago, { count: 3 })).toBe("3m ago");
    expect(t(($) => $.time.minutes_ago, { count: 5 })).toBe("5m ago");

    // A resource reload announces itself with languageChanged (I18nProvider).
    await act(async () => {
      i18n.addResourceBundle("en", "common", { daemon_setup: { loading: "Hi" } }, true, true);
      i18n.emit("languageChanged", "en");
    });
    expect(result.current.t(($) => $.daemon_setup.loading)).toBe("Hi");
  });
});
