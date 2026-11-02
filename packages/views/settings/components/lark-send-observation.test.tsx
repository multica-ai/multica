import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { api } from "@multica/core/api";
import type { LarkSendObservationResponse } from "@multica/core/types";
import enSettings from "../../locales/en/settings.json";
import enCommon from "../../locales/en/common.json";
import { LarkSendObservation } from "./lark-send-observation";

vi.mock("@multica/core/api", () => ({ api: { getLarkSendObservation: vi.fn() } }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  render(<QueryClientProvider client={client}><I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
    <LarkSendObservation wsId="ws" installationId="inst" />
  </I18nProvider></QueryClientProvider>);
  return userEvent.setup();
}

it.each([
  [null, "No saved observation for this binding."],
  [{ observed_at: "2026-11-02T00:00:00Z", result: "api_confirmed", reason: "none" }, "API confirmed the request"],
  [{ observed_at: "2026-11-02T00:00:00Z", result: "rejected", reason: "missing_permission" }, "Check the permissions required by the reply API and the published app version."],
  [{ observed_at: "2026-11-02T00:00:00Z", result: "uncertain", reason: "unconfirmed" }, "Check the original conversation before sending again to avoid duplicates."],
] as const)("reads and displays observation only after expansion: %j", async (observation, expected) => {
  vi.mocked(api.getLarkSendObservation).mockResolvedValue({ available: true, observation });
  const user = mount();
  expect(api.getLarkSendObservation).not.toHaveBeenCalled();
  await user.click(screen.getByText("Last text/card send observation"));
  expect(await screen.findByText(expected)).toBeInTheDocument();
  expect(api.getLarkSendObservation).toHaveBeenCalledWith("ws", "inst");
  expect(screen.getByText(enSettings.lark.send_observation_scope)).toBeInTheDocument();
});

it.each(["forbidden", "malformed"])("does not show missing evidence or stale confirmation for %s", async (mode) => {
  if (mode === "forbidden") vi.mocked(api.getLarkSendObservation).mockRejectedValue(new Error("secret provider text"));
  else vi.mocked(api.getLarkSendObservation).mockResolvedValue({ available: false, observation: null } satisfies LarkSendObservationResponse);
  const user = mount();
  await user.click(screen.getByText("Last text/card send observation"));
  await waitFor(() => expect(screen.getByText("Observation could not be loaded.")).toBeInTheDocument());
  expect(screen.queryByText("No saved observation for this binding.")).not.toBeInTheDocument();
  expect(screen.queryByText("secret provider text")).not.toBeInTheDocument();
});

it("does not show cached evidence from the previous binding during a rebind fetch", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>{children}</I18nProvider>
  </QueryClientProvider>;
  vi.mocked(api.getLarkSendObservation).mockResolvedValueOnce({ available: true, observation: { observed_at: "2026-11-02T00:00:00Z", result: "api_confirmed", reason: "none" } });
  const { rerender } = render(<LarkSendObservation wsId="ws" installationId="inst" revision="old" />, { wrapper });
  await userEvent.setup().click(screen.getByText("Last text/card send observation"));
  expect(await screen.findByText("API confirmed the request")).toBeInTheDocument();
  let resolve: (value: LarkSendObservationResponse) => void = () => {};
  vi.mocked(api.getLarkSendObservation).mockReturnValueOnce(new Promise((r) => { resolve = r; }));
  rerender(<LarkSendObservation wsId="ws" installationId="inst" revision="new" />);
  await waitFor(() => expect(api.getLarkSendObservation).toHaveBeenCalledTimes(2));
  expect(screen.queryByText("API confirmed the request")).not.toBeInTheDocument();
  resolve({ available: true, observation: null });
  expect(await screen.findByText("No saved observation for this binding.")).toBeInTheDocument();
});
