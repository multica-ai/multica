import { render, type RenderOptions, type RenderResult } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ReactElement, ReactNode } from "react";

// ee adds its own bundle to the app's i18n instance at runtime, so tests
// start from an empty catalog.
export function renderWithI18n(ui: ReactElement, options: Omit<RenderOptions, "wrapper"> = {}): RenderResult {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale="en" resources={{ en: {} }}>
        {children}
      </I18nProvider>
    );
  }
  return render(ui, { wrapper: Wrapper, ...options });
}
