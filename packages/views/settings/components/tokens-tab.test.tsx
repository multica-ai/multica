// Smoke test that catches the "tokens tab is entirely blank" regression.
// The page was hydrating to nothing in production after the upstream
// i18n + MCP merges; this test renders TokensTab against the same locale
// resources the deployed app loads, so any structural / hook / import
// failure surfaces as a Vitest failure instead of a blank screen.

import type { ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enSettings from "../../locales/en/settings.json";

vi.mock("@multica/core/api", () => ({
  api: {
    listPersonalAccessTokens: vi.fn().mockResolvedValue([]),
    createPersonalAccessToken: vi.fn(),
    revokePersonalAccessToken: vi.fn(),
    getBaseUrl: () => "https://example.com",
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { TokensTab } from "./tokens-tab";

function renderWithI18n(ui: ReactNode) {
  return render(
    <I18nProvider
      locale="en"
      resources={{ en: { common: enCommon, auth: enAuth, settings: enSettings } }}
    >
      {ui}
    </I18nProvider>,
  );
}

describe("TokensTab", () => {
  it("renders the section header without crashing", async () => {
    renderWithI18n(<TokensTab />);
    // The "API Tokens" string lives in en/settings.json under tokens.title.
    // If hydration crashes the panel goes blank — this assertion catches it.
    await waitFor(() => expect(screen.getByText("API Tokens")).toBeInTheDocument());
  });

  it("renders the create-token form (name input + create button)", async () => {
    renderWithI18n(<TokensTab />);
    await waitFor(() => {
      // Placeholder is in settings.tokens.name_placeholder.
      expect(screen.getByPlaceholderText(/Token name/i)).toBeInTheDocument();
    });
  });
});
