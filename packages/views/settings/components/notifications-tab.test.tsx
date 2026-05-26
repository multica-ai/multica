import { describe, expect, it, beforeEach, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const apiMock = vi.hoisted(() => ({
  listNotificationBindings: vi.fn(),
  listNotificationPreferences: vi.fn(),
  listNotificationWebhooks: vi.fn(),
  getAutoSubscribePreferences: vi.fn(),
  updateAutoSubscribePreferences: vi.fn(),
  createNotificationWebhook: vi.fn(),
  deleteNotificationWebhook: vi.fn(),
  testNotificationWebhook: vi.fn(),
  updateNotificationPreference: vi.fn(),
  listRuntimes: vi.fn(),
  bindOpenclawWeixin: vi.fn(),
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

const openclawWeixinBindPrompt = "先执行 `curl -fsSL https://multica.wujieai.com/install.sh | sh -s -- --restart` 将 multica cli 更新到最新版并重启 daemon，然后用 multica notify bind-wechat 绑定微信通知";

function makePreference(
  channel: string,
  eventType: string,
  enabled = false,
  requiresBinding = false,
) {
  return {
    channel,
    event_type: eventType,
    enabled,
    binding_id: null,
    requires_binding: requiresBinding,
    render_mode: "auto",
  };
}

function makeDefaultPreferences() {
  return [
    makePreference("notification_trigger", "mentioned", true),
    makePreference("notification_trigger", "replied", false),
    makePreference("notification_trigger", "issue_assigned", false),
    makePreference("notification_trigger", "subscribed_issue_updated", false),
    makePreference("notification_trigger", "task_completed", false),
    makePreference("notification_trigger", "task_failed", false),
    makePreference("inbox", "channel_enabled", true),
    makePreference("inbox", "mentioned", true),
    makePreference("dingtalk", "channel_enabled", false, true),
    makePreference("dingtalk", "mentioned", false, true),
    makePreference("dingtalk", "task_completed", false, true),
    makePreference("dingtalk", "task_failed", false, true),
    makePreference("email", "channel_enabled", false, true),
    makePreference("email", "mentioned", false, true),
    makePreference("custom_webhook", "channel_enabled", false),
    makePreference("custom_webhook", "mentioned", false),
    makePreference("custom_webhook", "issue_assigned", false),
    makePreference("custom_webhook", "subscribed_issue_updated", false),
    makePreference("openclaw_weixin", "channel_enabled", false, true),
    makePreference("openclaw_weixin", "mentioned", false, true),
    makePreference("openclaw_weixin", "replied", false, true),
    makePreference("openclaw_weixin", "task_completed", false, true),
    makePreference("openclaw_weixin", "task_failed", false, true),
  ];
}

function makeDefaultAutoSubscribePreferences() {
  return {
    issue_creator: true,
    issue_assignee: true,
    comment_author: true,
    issue_description_mention: false,
    comment_mention: false,
    quick_create_requester: true,
  };
}

describe("NotificationsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.listNotificationBindings.mockResolvedValue({ bindings: [] });
    apiMock.listNotificationWebhooks.mockResolvedValue({ webhooks: [] });
    apiMock.listNotificationPreferences.mockResolvedValue({
      preferences: makeDefaultPreferences(),
    });
    apiMock.getAutoSubscribePreferences.mockResolvedValue({
      workspace_id: "workspace-1",
      preferences: makeDefaultAutoSubscribePreferences(),
    });
    apiMock.updateAutoSubscribePreferences.mockImplementation(async (preferences) => ({
      workspace_id: "workspace-1",
      preferences: {
        ...makeDefaultAutoSubscribePreferences(),
        ...preferences,
      },
    }));
    apiMock.listRuntimes.mockResolvedValue([]);
    apiMock.updateNotificationPreference.mockImplementation(async (payload) => ({
      ...payload,
      binding_id: null,
      requires_binding: payload.channel === "dingtalk",
      render_mode: payload.render_mode ?? "auto",
    }));
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    });
  });

  it("shows not connected state for dingtalk and keeps the channel switch disabled", async () => {
    render(<NotificationsTab />);

    expect(await screen.findByText("DingTalk")).toBeInTheDocument();
    expect(screen.getAllByText(/Profile → Linked Accounts/).length).toBeGreaterThan(0);

    expect(screen.getByRole("switch", { name: "Toggle channel DingTalk" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("updates a trigger independently from channel switches", async () => {
    const user = userEvent.setup();
    render(<NotificationsTab />);

    const repliedTrigger = await screen.findByRole("switch", {
      name: "Toggle trigger 被回复时",
    });
    const inboxChannel = screen.getByRole("switch", {
      name: "Toggle channel Inbox",
    });
    expect(repliedTrigger).toHaveAttribute("aria-checked", "false");
    expect(inboxChannel).toHaveAttribute("aria-checked", "true");

    await user.click(repliedTrigger);

    await waitFor(() => {
      expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
        channel: "notification_trigger",
        event_type: "replied",
        enabled: true,
      });
    });
    expect(apiMock.updateNotificationPreference).not.toHaveBeenCalledWith({
      channel: "inbox",
      event_type: "channel_enabled",
      enabled: false,
    });
  });

  it("toggles one channel from the already selected trigger set", async () => {
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
      name: "Toggle channel Custom Webhook",
    });
    const mentionedTrigger = screen.getByRole("switch", {
      name: "Toggle trigger 被 @提及时",
    });
    const issueAssignedTrigger = screen.getByRole("switch", {
      name: "Toggle trigger 被分配 Issue 时",
    });
    expect(mentionedTrigger).toHaveAttribute("aria-checked", "true");
    expect(issueAssignedTrigger).toHaveAttribute("aria-checked", "false");

    await user.click(customSwitch);

    await waitFor(() => {
      expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
        channel: "custom_webhook",
        event_type: "channel_enabled",
        enabled: true,
      });
    });
    expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
      channel: "custom_webhook",
      event_type: "mentioned",
      enabled: true,
    });
    expect(apiMock.updateNotificationPreference).not.toHaveBeenCalledWith({
      channel: "custom_webhook",
      event_type: "issue_assigned",
      enabled: true,
    });
    expect(issueAssignedTrigger).toHaveAttribute("aria-checked", "false");
  });

  it("keeps trigger switches available when every channel is off", async () => {
    const user = userEvent.setup();
    apiMock.listNotificationPreferences.mockResolvedValue({
      preferences: makeDefaultPreferences().map((pref) =>
        pref.channel === "notification_trigger" && pref.event_type === "task_completed"
          ? { ...pref, enabled: true }
          : pref,
      ),
    });

    render(<NotificationsTab />);

    const taskCompletedTrigger = await screen.findByRole("switch", {
      name: "Toggle trigger Agent 任务完成时",
    });
    expect(taskCompletedTrigger).toHaveAttribute("aria-checked", "true");

    await user.click(taskCompletedTrigger);

    await waitFor(() => {
      expect(apiMock.updateNotificationPreference).toHaveBeenCalledWith({
        channel: "notification_trigger",
        event_type: "task_completed",
        enabled: false,
      });
    });
    expect(apiMock.updateNotificationPreference).not.toHaveBeenCalledWith({
      channel: "dingtalk",
      event_type: "channel_enabled",
      enabled: true,
    });
  });

  it("renders and updates auto-subscribe preferences independently", async () => {
    const user = userEvent.setup();
    render(<NotificationsTab />);

    expect(await screen.findByText("自动关注")).toBeInTheDocument();
    expect(screen.getByText("这些开关只决定是否把你加入 Issue 关注者列表，不会关闭 @ 提醒本身。")).toBeInTheDocument();

    const descriptionMention = screen.getByRole("switch", {
      name: "Toggle auto subscribe 正文/描述中 @我",
    });
    const triggerMention = screen.getByRole("switch", {
      name: "Toggle trigger 被 @提及时",
    });
    expect(descriptionMention).toHaveAttribute("aria-checked", "false");
    expect(triggerMention).toHaveAttribute("aria-checked", "true");

    await user.click(descriptionMention);

    await waitFor(() => {
      expect(apiMock.updateAutoSubscribePreferences).toHaveBeenCalledWith({
        issue_description_mention: true,
      });
    });
    expect(apiMock.updateNotificationPreference).not.toHaveBeenCalled();
  });

  it("copies the concise OpenClaw WeChat bind prompt", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText,
      },
    });
    apiMock.listRuntimes.mockResolvedValue([{ id: "runtime-1", provider: "openclaw", status: "online" }]);

    render(<NotificationsTab />);

    expect(await screen.findByText("发送下面这句话给你的 OpenClaw 助手")).toBeInTheDocument();
    expect(screen.getByText(openclawWeixinBindPrompt)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "复制微信绑定指令" }));

    expect(writeText).toHaveBeenCalledWith(openclawWeixinBindPrompt);
    expect(screen.getByText("发送后刷新此页面即可看到绑定结果")).toBeInTheDocument();
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
