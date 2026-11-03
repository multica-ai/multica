import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import translations from "../../../../../../packages/views/locales/en/settings.json";
import type { DaemonStatus } from "../../../shared/daemon-types";

const mocks = vi.hoisted(() => ({ deviceLink: vi.fn(), setDeviceLink: vi.fn(), servicePreview: vi.fn(async () => ({ available: true, enabled: false, ports: [] })), setServicePreview: vi.fn(), conversationPreheat: vi.fn(), setConversationPreheat: vi.fn() }));
vi.mock("@multica/views/i18n", () => ({ useT: () => ({ t: (selector: (resources: typeof translations) => string) => selector(translations) }) }));
vi.mock("../platform/local-v2-transport", () => ({ localV2Transport: () => mocks }));
import { DeviceLinkSettingsTab, deviceLinkSettingKey } from "./device-link-settings-tab";

const copy = translations.desktop.device_link;
const renderTab = (client = new QueryClient()) => render(<QueryClientProvider client={client}><DeviceLinkSettingsTab /></QueryClientProvider>);

describe("Device link settings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.deviceLink.mockResolvedValue({ available: true, enabled: false });
    mocks.conversationPreheat.mockResolvedValue({ old_conversations: true });
  });

  it("turns off early starts for opened conversations on this device", async () => {
    const preheat = translations.desktop.conversation_preheat;
    mocks.setConversationPreheat.mockResolvedValueOnce({ old_conversations: false });
    renderTab();
    const toggle = await screen.findByRole("switch", { name: preheat.label });
    await waitFor(() => expect(toggle).toBeChecked());
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.setConversationPreheat).toHaveBeenCalledWith(false));
    await waitFor(() => expect(toggle).not.toBeChecked());
  });

  it("stays unavailable when the daemon does not have the setting", async () => {
    mocks.conversationPreheat.mockRejectedValueOnce(new Error("http_404"));
    renderTab();
    const toggle = await screen.findByRole("switch", { name: translations.desktop.conversation_preheat.label });
    await waitFor(() => expect(mocks.conversationPreheat).toHaveBeenCalled());
    expect(toggle).toHaveAttribute("data-disabled");
  });

  it("asks before letting other devices in and stays off until the daemon accepts", async () => {
    let finish!: (value: unknown) => void;
    mocks.setDeviceLink.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    renderTab();
    const toggle = await screen.findByRole("switch", { name: copy.allow });
    await waitFor(() => expect(toggle).not.toHaveAttribute("data-disabled"));
    fireEvent.click(toggle);
    expect(await screen.findByRole("alertdialog")).toHaveTextContent(copy.confirm_title);
    expect(mocks.setDeviceLink).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: copy.confirm }));
    await waitFor(() => expect(mocks.setDeviceLink).toHaveBeenCalledWith(true));
    // The modal hides the page from assistive technology meanwhile.
    expect(screen.getByRole("switch", { name: copy.allow, hidden: true })).not.toBeChecked();
    finish({ available: true, enabled: true });
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
    expect(screen.getByRole("switch", { name: copy.allow })).toBeChecked();
  });

  it("keeps the dialog open with an error when turning on fails", async () => {
    mocks.setDeviceLink.mockRejectedValueOnce(new Error("disk full"));
    renderTab();
    const toggle = await screen.findByRole("switch", { name: copy.allow });
    await waitFor(() => expect(toggle).not.toHaveAttribute("data-disabled"));
    fireEvent.click(toggle);
    fireEvent.click(await screen.findByRole("button", { name: copy.confirm }));
    expect(await screen.findByRole("alert")).toHaveTextContent(copy.save_failed);
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: copy.cancel }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  it("turns off without confirmation", async () => {
    mocks.deviceLink.mockResolvedValue({ available: true, enabled: true });
    mocks.setDeviceLink.mockResolvedValue({ available: true, enabled: false });
    renderTab();
    const toggle = await screen.findByRole("switch", { name: copy.allow });
    await waitFor(() => expect(toggle).toBeChecked());
    fireEvent.click(toggle);
    await waitFor(() => expect(mocks.setDeviceLink).toHaveBeenCalledWith(false));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("switch", { name: copy.allow })).not.toBeChecked());
  });

  it.each([false, true])("refreshes a stale unavailable setting after daemon startup (request failed: %s)", async (failed) => {
    const listeners = new Set<(status: DaemonStatus) => void>();
    const previousAPI = Object.getOwnPropertyDescriptor(window, "daemonAPI");
    Object.defineProperty(window, "daemonAPI", { configurable: true, value: {
      onStatusChange: (listener: (status: DaemonStatus) => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    } });
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    if (failed) mocks.deviceLink.mockRejectedValueOnce(new Error("Runtime starting"));
    else mocks.deviceLink.mockResolvedValueOnce({ available: false, enabled: false });
    const view = renderTab(client);
    try {
      await waitFor(() => expect(client.getQueryState(deviceLinkSettingKey)?.status).toBe(failed ? "error" : "success"));
      const toggle = screen.getByRole("switch", { name: copy.allow });
      expect(toggle).toHaveAttribute("data-disabled");
      await act(async () => { for (const listener of listeners) listener({ state: "running" }); });
      await waitFor(() => expect(toggle).not.toHaveAttribute("data-disabled"));
      expect(toggle).not.toBeChecked();
      expect(mocks.setDeviceLink).not.toHaveBeenCalled();
      const reads = mocks.deviceLink.mock.calls.length;
      await act(async () => { for (const listener of listeners) listener({ state: "running" }); });
      expect(mocks.deviceLink).toHaveBeenCalledTimes(reads);
    } finally {
      view.unmount(); client.clear();
      if (previousAPI) Object.defineProperty(window, "daemonAPI", previousAPI);
      else Reflect.deleteProperty(window, "daemonAPI");
    }
    expect(listeners.size).toBe(0);
  });
});
