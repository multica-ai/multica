import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { AppAccountDialog } from "./app-account-dialog";

vi.mock("@multica/core/api", () => ({ api: {
  getAppRules: vi.fn(), listAppTools: vi.fn(), listAppActivity: vi.fn(),
} }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("opens account activity directly without fetching or showing permission settings", async () => {
  vi.mocked(api.getAppRules).mockResolvedValue({ rules: [] });
  vi.mocked(api.listAppTools).mockResolvedValue({ tools: [] });
  vi.mocked(api.listAppActivity).mockResolvedValue({ calls: [{
    id: "call-1", agent_id: "a1", issue_id: null, chat_session_id: null, triggered_by: null,
    tool_slug: "GMAIL_SEND_EMAIL", action_class: "send", outcome: "ok", error: null,
    created_at: "2026-11-09T10:00:00Z",
  }] });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
        <AppAccountDialog scope="personal" slug="gmail" name="Gmail" onClose={vi.fn()} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  expect(await screen.findByText("GMAIL_SEND_EMAIL")).toBeVisible();
  expect(api.listAppActivity).toHaveBeenCalledWith("gmail", "personal");
  expect(api.getAppRules).not.toHaveBeenCalled();
  expect(api.listAppTools).not.toHaveBeenCalled();
  expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
});
