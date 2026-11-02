/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { BriefPushPreference } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { OfficeBriefDelivery } from "./office-brief-delivery";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/api", () => ({
  api: {
    getBriefPushPreference: mocks.get,
    updateBriefPushPreference: mocks.update,
  },
}));

const LINKED: BriefPushPreference = { brief_push: false, binding_id: "b1", quiet_hours: null };

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <OfficeBriefDelivery />
    </QueryClientProvider>,
  );
}

const pushSwitch = () => screen.findByRole("switch", { name: "Send the brief to my chat" });
const quietSwitch = () => screen.findByRole("switch", { name: "Quiet hours" });
const saveButton = () => screen.getByRole("button", { name: "Save" });

beforeEach(() => {
  mocks.get.mockResolvedValue(LINKED);
  mocks.update.mockImplementation(async (data: BriefPushPreference) => data);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("OfficeBriefDelivery", () => {
  it("explains that a chat must be linked, and keeps push off until one is", async () => {
    mocks.get.mockResolvedValue({ brief_push: false, binding_id: null, quiet_hours: null });
    renderPanel();

    expect(await screen.findByText("Link your Telegram chat first. Briefs go only to a linked chat.")).toBeTruthy();
    const toggle = await pushSwitch();
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(toggle.hasAttribute("data-disabled")).toBe(true);
  });

  it("sends the whole preference when only the push switch changes", async () => {
    mocks.get.mockResolvedValue({ brief_push: false, binding_id: "b1", quiet_hours: { start: 22, end: 7 } });
    renderPanel();

    fireEvent.click(await pushSwitch());
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({
      brief_push: true,
      binding_id: "b1",
      quiet_hours: { start: 22, end: 7 },
    });
  });

  it("turns quiet hours on with the defaults and sends the chosen start", async () => {
    renderPanel();

    fireEvent.click(await quietSwitch());
    fireEvent.change(screen.getByLabelText("From"), { target: { value: "21" } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({
      brief_push: false,
      binding_id: "b1",
      quiet_hours: { start: 21, end: 7 },
    });
  });

  it("refuses quiet hours whose start and end are the same", async () => {
    renderPanel();

    fireEvent.click(await quietSwitch());
    fireEvent.change(screen.getByLabelText("Until"), { target: { value: "22" } });

    expect(screen.getByRole("alert").textContent).toBe("Pick two different hours.");
    expect((saveButton() as HTMLButtonElement).disabled).toBe(true);
  });

  it("turns quiet hours off by sending no window", async () => {
    mocks.get.mockResolvedValue({ brief_push: false, binding_id: "b1", quiet_hours: { start: 22, end: 7 } });
    renderPanel();

    fireEvent.click(await quietSwitch());
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({ brief_push: false, binding_id: "b1", quiet_hours: null });
  });

  it("keeps the save disabled until something changes", async () => {
    renderPanel();

    await pushSwitch();
    expect((saveButton() as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows a load error when the preference cannot be read", async () => {
    mocks.get.mockRejectedValue(new Error("network down"));
    renderPanel();

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("Could not load delivery settings")).toBeTruthy();
  });
});
