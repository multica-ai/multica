// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime } from "@multica/core/types";
import { expect, it, vi } from "vitest";
import enRuntimes from "../../locales/en/runtimes.json";
import { canShowSubscriptionQuota, SubscriptionQuotaCard, SubscriptionQuotaSummary } from "./subscription-quota";

const quotaRequest = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: { runtimeSubscriptionQuota: quotaRequest },
}));

const runtime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "workspace-1",
  daemon_id: "daemon-1",
  name: "Codex",
  runtime_mode: "local",
  provider: "codex",
  launch_header: "",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: "owner-1",
  visibility: "public",
  last_seen_at: null,
  created_at: "",
  updated_at: "",
};

const now = 1_800_000_000_000;
const snapshot = {
  observedAt: now / 1000,
  primary: { windowDurationMins: 180, usedPercent: 25, resetsAt: now / 1000 + 100 },
};

it("checks quota on open and lets the owner request a fresh check", async () => {
  quotaRequest.mockResolvedValueOnce({ status: "idle" })
    .mockResolvedValueOnce({ status: "complete", snapshot })
    .mockResolvedValueOnce({ status: "unavailable" });
  const client = new QueryClient();

  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
        <SubscriptionQuotaCard runtime={runtime} wsId={runtime.workspace_id} now={now} online />
      </I18nProvider>
    </QueryClientProvider>,
  );

  await screen.findByText("25% used");
  expect(quotaRequest).toHaveBeenNthCalledWith(2, runtime.id, "POST", true);
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(quotaRequest).toHaveBeenNthCalledWith(3, runtime.id, "POST", false));
  await screen.findByText("No ChatGPT subscription quota for this login.");
  expect(screen.queryByText("25% used")).toBeNull();
});

it("marks reset windows as stale and keeps quota out of shared runtimes", () => {
  expect(canShowSubscriptionQuota(runtime, "another-user")).toBe(false);
  expect(canShowSubscriptionQuota({ ...runtime, profile_id: "custom" }, runtime.owner_id!)).toBe(false);
  expect(canShowSubscriptionQuota(runtime, runtime.owner_id!)).toBe(true);

  render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <SubscriptionQuotaSummary snapshot={snapshot} now={now + 100_000} />
    </I18nProvider>,
  );
  expect(screen.getByText("Out of date")).toBeTruthy();
});

it("shows upgrade guidance without trying to refresh on an older daemon", async () => {
  quotaRequest.mockClear().mockResolvedValueOnce({ status: "unsupported" });
  const client = new QueryClient();
  render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
        <SubscriptionQuotaCard runtime={runtime} wsId={runtime.workspace_id} now={now} online />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await screen.findByText("Update Multica on this machine to refresh quota.");
  expect((screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(true);
  expect(quotaRequest).toHaveBeenCalledTimes(1);
});

it("shows DSH task exhaustion without a usage meter or manual refresh", () => {
  quotaRequest.mockClear();
  render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <SubscriptionQuotaCard
        runtime={{ ...runtime, provider: "dsh", subscription_quota: { observedAt: now / 1000, exhausted: true } }}
        wsId={runtime.workspace_id}
        now={now}
        online
      />
    </I18nProvider>,
  );
  expect(screen.getByText("DSH quota")).toBeTruthy();
  expect(screen.getByText("Quota exhausted")).toBeTruthy();
  expect(screen.queryByRole("progressbar")).toBeNull();
  expect(screen.queryByRole("button", { name: "Refresh" })).toBeNull();
  expect(quotaRequest).not.toHaveBeenCalled();
});
