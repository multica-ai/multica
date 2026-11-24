import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { notificationPreferenceKeys } from "@multica/core/notification-preferences/queries";
import enSettings from "../../locales/en/settings.json";
import { useSettingsSearchIndex } from "./use-settings-search-index";

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-a", name: "Acme" }),
}));

function squadHandoffsAnchor(preferences?: Record<string, string>) {
  const qc = new QueryClient();
  if (preferences) {
    qc.setQueryData(notificationPreferenceKeys.all("ws-a"), {
      workspace_id: "ws-a",
      preferences,
      auto_archive_prompt: false,
    });
  }
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={{ en: { settings: enSettings } }}>
        {children}
      </I18nProvider>
    </QueryClientProvider>
  );
  const { result } = renderHook(
    () => useSettingsSearchIndex([{ value: "notifications", label: "Notifications" }]),
    { wrapper },
  );
  return result.current.find((entry) => entry.title === "Include squad handoffs")?.anchor;
}

describe("squad handoffs in settings search", () => {
  it("lands on its own row while comments are on", () => {
    expect(squadHandoffsAnchor({})).toBe("squad_handoffs");
    expect(squadHandoffsAnchor()).toBe("squad_handoffs");
  });

  it("lands on Comments while the row is hidden", () => {
    expect(squadHandoffsAnchor({ comments: "muted" })).toBe("comments");
  });
});
