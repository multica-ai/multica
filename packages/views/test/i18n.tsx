import {
  render,
  type RenderOptions,
  type RenderResult,
} from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ReactElement, ReactNode } from "react";
import { RESOURCES } from "../locales";

// Single i18n test wrapper for the whole package. Wraps the production
// `RESOURCES` map (every namespace registered there is available to the
// component under test) so when a new namespace lands the test never
// silently renders translation keys-as-text — the test sees the same
// resource set users do. The previous pattern of inlining a per-file
// `TEST_RESOURCES` slice meant every test author had to remember to
// extend the slice when their component started using a new namespace.
//
// Use `renderWithI18n` like the standard `render`. Pass `locale: "zh-Hans"`
// to verify Chinese strings; default is "en".
type RenderArgs = Omit<RenderOptions, "wrapper"> & {
  locale?: "en" | "zh-Hans";
};

export function renderWithI18n(
  ui: ReactElement,
  options: RenderArgs = {},
): RenderResult {
  const { locale = "en", ...rest } = options;
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale={locale} resources={RESOURCES}>
        {children}
      </I18nProvider>
    );
  }
  return render(ui, { wrapper: Wrapper, ...rest });
}

export { RESOURCES };
