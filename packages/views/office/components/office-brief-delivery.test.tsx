/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  BriefPushBindingListSchema,
  type BriefPushBinding,
  type BriefPushPreference,
} from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { OfficeBriefDelivery } from "./office-brief-delivery";

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
  list: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getBriefPushPreference: mocks.get,
    updateBriefPushPreference: mocks.update,
    listBriefPushBindings: mocks.list,
  },
}));

const TELEGRAM_BOT: BriefPushBinding = { id: "b1", channel: "telegram", label: "Ops room" };
const LINKED: BriefPushPreference = { brief_push: false, binding_id: "b1", quiet_hours: null };
const UNLINKED: BriefPushPreference = { brief_push: false, binding_id: null, quiet_hours: null };

const adapter: NavigationAdapter = {
  pathname: "/acme/office",
  searchParams: new URLSearchParams(),
  hash: "",
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  getShareableUrl: (path) => path,
};

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <OfficeBriefDelivery />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

const pushSwitch = () => screen.findByRole("switch", { name: "Send the brief to my chat" });
const quietSwitch = () => screen.findByRole("switch", { name: "Quiet hours" });
const saveButton = () => screen.getByRole("button", { name: "Save" });
const chatSelect = () => screen.findByLabelText("Chat") as Promise<HTMLSelectElement>;

beforeEach(() => {
  mocks.get.mockResolvedValue(LINKED);
  mocks.list.mockResolvedValue([TELEGRAM_BOT]);
  mocks.update.mockImplementation(async (data: BriefPushPreference) => data);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("OfficeBriefDelivery", () => {
  it("links to the channel settings when no chat is linked, and keeps push off", async () => {
    mocks.get.mockResolvedValue(UNLINKED);
    mocks.list.mockResolvedValue([]);
    renderPanel();

    const link = await screen.findByRole("link", {
      name: "Link your Telegram chat first. Briefs go only to a linked chat.",
    });
    expect(link.getAttribute("href")).toBe("/acme/settings?tab=channels&integration=telegram");
    expect(screen.queryByLabelText("Chat")).toBeNull();

    const toggle = await pushSwitch();
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(toggle.hasAttribute("data-disabled")).toBe(true);
  });

  it("treats a malformed chat list as no chat", async () => {
    mocks.get.mockResolvedValue(UNLINKED);
    // What the client yields for a body that is not a list of chats.
    mocks.list.mockImplementation(async () =>
      parseWithFallback({ bindings: "telegram" }, BriefPushBindingListSchema, [], { endpoint: "test" }),
    );
    renderPanel();

    expect(await screen.findByRole("link", { name: /Link your Telegram chat first/ })).toBeTruthy();
    expect((await pushSwitch()).hasAttribute("data-disabled")).toBe(true);
  });

  it("offers the listed chats with the channel name and label, and enables push once one is picked", async () => {
    mocks.get.mockResolvedValue(UNLINKED);
    renderPanel();

    const select = await chatSelect();
    expect(select.value).toBe("");
    expect(Array.from(select.options).map((option) => option.textContent)).toEqual([
      "Choose a chat",
      "Telegram · Ops room",
    ]);
    expect((await pushSwitch()).hasAttribute("data-disabled")).toBe(true);

    fireEvent.change(select, { target: { value: "b1" } });
    fireEvent.click(await pushSwitch());
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({ brief_push: true, binding_id: "b1", quiet_hours: null });
  });

  it("names DingTalk, WeCom and Lark chats by their settings titles", async () => {
    mocks.get.mockResolvedValue(UNLINKED);
    mocks.list.mockResolvedValue([
      { id: "d1", channel: "dingtalk", label: "" },
      { id: "w1", channel: "wecom", label: "Ops" },
      { id: "f1", channel: "feishu", label: "" },
    ] satisfies BriefPushBinding[]);
    renderPanel();

    const select = await chatSelect();
    expect(Array.from(select.options).map((option) => option.textContent)).toEqual([
      "Choose a chat",
      "DingTalk",
      "WeCom · Ops",
      "Lark",
    ]);
  });

  it("pre-selects the saved chat and sends its id when the push switch changes", async () => {
    renderPanel();

    const select = await chatSelect();
    expect(select.value).toBe("b1");
    expect((await pushSwitch()).hasAttribute("data-disabled")).toBe(false);

    fireEvent.click(await pushSwitch());
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({ brief_push: true, binding_id: "b1", quiet_hours: null });
  });

  it("reads a saved chat that is no longer listed as none, so push stays off", async () => {
    mocks.get.mockResolvedValue({ brief_push: true, binding_id: "gone", quiet_hours: null });
    renderPanel();

    expect((await chatSelect()).value).toBe("");
    expect((await pushSwitch()).hasAttribute("data-disabled")).toBe(true);
    expect((saveButton() as HTMLButtonElement).disabled).toBe(true);

    // The next save, for any change, sends the chat as cleared.
    fireEvent.click(await quietSwitch());
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({
      brief_push: false,
      binding_id: null,
      quiet_hours: { start: 22, end: 7 },
    });
  });

  it("sends the whole preference when only the quiet hours change", async () => {
    mocks.get.mockResolvedValue({ brief_push: false, binding_id: "b1", quiet_hours: { start: 22, end: 7 } });
    renderPanel();

    fireEvent.click(await quietSwitch());
    fireEvent.click(saveButton());

    await waitFor(() => expect(mocks.update).toHaveBeenCalledTimes(1));
    expect(mocks.update).toHaveBeenCalledWith({ brief_push: false, binding_id: "b1", quiet_hours: null });
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

  it("keeps the save disabled until something changes", async () => {
    renderPanel();

    await pushSwitch();
    expect((saveButton() as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows a load error when the chat list cannot be read", async () => {
    mocks.list.mockRejectedValue(new Error("network down"));
    renderPanel();

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("Could not load delivery settings")).toBeTruthy();
  });

  it("shows a load error when the preference cannot be read", async () => {
    mocks.get.mockRejectedValue(new Error("network down"));
    renderPanel();

    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText("Could not load delivery settings")).toBeTruthy();
  });
});
