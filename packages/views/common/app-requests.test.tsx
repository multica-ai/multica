import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { workspaceFeatureKeys } from "@multica/core/billing";
import { toast } from "sonner";
import enCommon from "../locales/en/common.json";
import enSettings from "../locales/en/settings.json";
import enBilling from "../locales/en/billing.json";
import { IssueAppRequests } from "./app-requests";

const nav = vi.hoisted(() => ({
  searchParams: new URLSearchParams(),
  pathname: "/acme/issues/MUL-1",
  hash: "",
  replace: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/config", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useFeatureEnabled: () => false,
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (select: (state: { user: { id: string } }) => unknown) => select({ user: { id: "user-1" } }),
    { getState: () => ({ user: { id: "user-1" } }) },
  ),
}));
vi.mock("../navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useNavigation: () => nav,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderCard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings, billing: enBilling } }}>
        <IssueAppRequests issueId="issue-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { invalidate };
}

it("explains a connect the plan refused and refetches the plan", async () => {
  nav.searchParams = new URLSearchParams(
    "app_connect_error=gmail&app_connect_error_reason=feature_locked&keep=1",
  );
  const { invalidate } = renderCard();
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith(enBilling.paid_feature.locked_error));
  expect(toast.error).not.toHaveBeenCalledWith(enSettings.connected_apps.toast_connect_failed);
  expect(invalidate).toHaveBeenCalledWith({ queryKey: workspaceFeatureKeys.all("ws-1") });
  expect(nav.replace).toHaveBeenCalledWith("/acme/issues/MUL-1?keep=1");
});

it("keeps the generic retry message when an older server sends no reason", async () => {
  nav.searchParams = new URLSearchParams("app_connect_error=notion");
  renderCard();
  await waitFor(() =>
    expect(toast.error).toHaveBeenCalledWith(enSettings.connected_apps.toast_connect_failed),
  );
  expect(toast.error).not.toHaveBeenCalledWith(enBilling.paid_feature.locked_error);
  expect(nav.replace).toHaveBeenCalledWith("/acme/issues/MUL-1");
});
