import { describe, expect, it, beforeEach, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const apiMock = vi.hoisted(() => ({
  listNotificationBindings: vi.fn(),
  listNotificationPreferences: vi.fn(),
  listNotificationWebhooks: vi.fn(),
  createNotificationWebhook: vi.fn(),
  deleteNotificationWebhook: vi.fn(),
  testNotificationWebhook: vi.fn(),
  updateNotificationPreference: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: apiMock,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { NotificationsTab } from "./notifications-tab";

describe("NotificationsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.listNotificationBindings.mockResolvedValue({ bindings: [] });
    apiMock.listNotificationWebhooks.mockResolvedValue({ webhooks: [] });
    apiMock.listNotificationPreferences.mockResolvedValue({
      preferences: [
        {
          channel: "inbox",
          event_type: "mentioned",
          enabled: true,
          binding_id: null,
          requires_binding: false,
        },
        {
          channel: "dingtalk",
          event_type: "mentioned",
          enabled: false,
          binding_id: null,
          requires_binding: true,
        },
        {
          channel: "custom_webhook",
          event_type: "mentioned",
          enabled: false,
          binding_id: null,
          requires_binding: false,
        },
        {
          channel: "custom_webhook",
          event_type: "issue_assigned",
          enabled: false,
          binding_id: null,
          requires_binding: false,
        },
        {
          channel: "custom_webhook",
          event_type: "subscribed_issue_updated",
          enabled: false,
          binding_id: null,
          requires_binding: false,
        },
      ],
    });
    apiMock.updateNotificationPreference.mockImplementation(async (payload) => ({
      ...payload,
      binding_id: null,
      requires_binding: payload.channel === "dingtalk",
    }));
  });

  it("shows not connected state for dingtalk and keeps the toggle disabled", async () => {
    render(<NotificationsTab />);

    expect(await screen.findByText("DingTalk")).toBeInTheDocument();
    expect(screen.getAllByText(/Profile → Linked Accounts/).length).toBeGreaterThan(0);

    const switches = screen.getAllByRole("switch");
    expect(switches).toHaveLength(3);
    expect(switches[1]).toHaveAttribute("aria-disabled", "true");
  });

  it("updates the inbox preference when the switch is toggled", async () => {
    const user = userEvent.setup();
    render(<NotificationsTab />);

    const inboxSwitch = await screen.findByRole("switch", {
      name: "Toggle Inbox",
    });
    expect(inboxSwitch).toHaveAttribute("aria-checked", "true");

    await user.click(inboxSwitch);

    await waitFor(() => {
      expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
        channel: "inbox",
        event_type: "mentioned",
        enabled: false,
      });
    });
  });

  it("shows one custom webhook channel switch and toggles all supported events", async () => {
    const user = userEvent.setup();
    apiMock.listNotificationWebhooks.mockResolvedValue({
      webhooks: [
        {
          id: "webhook-1",
          name: "GTD",
          masked_url: "https://example.com/***",
          enabled: true,
          workspace_id: null,
          payload_template: "",
          content_prefix: "",
          created_at: "2026-05-07T00:00:00Z",
          updated_at: "2026-05-07T00:00:00Z",
        },
      ],
    });

    render(<NotificationsTab />);

    const customSwitch = await screen.findByRole("switch", {
      name: "Toggle Custom Webhook",
    });
    expect(screen.queryByText("@ mentions")).not.toBeInTheDocument();
    expect(screen.queryByText("Assigned to me")).not.toBeInTheDocument();
    expect(screen.queryByText("Subscribed issue updates")).not.toBeInTheDocument();

    await user.click(customSwitch);

    await waitFor(() => {
      expect(apiMock.updateNotificationPreference).toHaveBeenCalledTimes(3);
    });
    expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
      channel: "custom_webhook",
      event_type: "mentioned",
      enabled: true,
    });
    expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
      channel: "custom_webhook",
      event_type: "issue_assigned",
      enabled: true,
    });
    expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
      channel: "custom_webhook",
      event_type: "subscribed_issue_updated",
      enabled: true,
    });
  });

  it("creates a webhook with custom payload template and no secret field", async () => {
    const user = userEvent.setup();
    apiMock.createNotificationWebhook.mockResolvedValue({
      id: "webhook-2",
      name: "DingTalk",
      masked_url: "https://oapi.dingtalk.com/***",
      enabled: true,
      workspace_id: null,
      payload_template: '{"msgtype":"text","text":{"content":"{{content}}"}}',
      content_prefix: "[Multica] ",
      created_at: "2026-05-07T00:00:00Z",
      updated_at: "2026-05-07T00:00:00Z",
    });

    render(<NotificationsTab />);

    await user.type(await screen.findByLabelText("Name"), "DingTalk");
    await user.type(screen.getByLabelText("URL"), "https://oapi.dingtalk.com/robot/send");
    fireEvent.change(screen.getByLabelText("Content prefix"), {
      target: { value: "[Multica] " },
    });
    fireEvent.change(screen.getByLabelText("Payload JSON template"), {
      target: { value: '{"msgtype":"text","text":{"content":"{{content}}"}}' },
    });

    expect(screen.queryByLabelText("Secret")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => {
      expect(apiMock.createNotificationWebhook).toHaveBeenCalledWith({
        name: "DingTalk",
        url: "https://oapi.dingtalk.com/robot/send",
        content_prefix: "[Multica] ",
        payload_template: '{"msgtype":"text","text":{"content":"{{content}}"}}',
      });
    });
  });
});
