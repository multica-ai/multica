import "./env";
import { expect, test } from "@playwright/test";
import type { AgentGuiInitSnapshot } from "../packages/tutti-agent-gui-host/src/protocol";

function mentionStyle(element: HTMLElement) {
  const root = element.querySelector<HTMLElement>('[data-slot="mention-pill"], .rich-text-at-mention-row--app') ?? element;
  const text = root.querySelector<HTMLElement>('.rich-text-at-mention-row__app-name') ?? root;
  const icon = root.querySelector('img')!;
  const style = getComputedStyle(root);
  const textStyle = getComputedStyle(text);
  return { fontSize: textStyle.fontSize, fontWeight: textStyle.fontWeight, color: textStyle.color, gap: style.gap,
    iconWidth: icon.getBoundingClientRect().width, iconHeight: icon.getBoundingClientRect().height, iconRadius: getComputedStyle(icon).borderRadius };
}

for (const theme of ["light", "dark"] as const) {
  test(`selects a member by identity and shows only their avatar and name in ${theme}`, async ({ page }) => {
    const at = "2026-11-06T00:00:00Z";
    const session = { id: "member-chat", workspace_id: "member-workspace", creator_id: "viewer", agent_id: "local:codex", title: "成员引用", status: "active" as const, has_unread: false, created_at: at, updated_at: at };
    const avatar = `data:image/svg+xml,${encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" rx="16" fill="#3b82f6"/><text x="16" y="22" text-anchor="middle" fill="white" font-size="20">A</text></svg>')}`;
    const snapshot: AgentGuiInitSnapshot = {
      workspaceId: session.workspace_id, workspaceSlug: "members", userId: "viewer", locale: "zh-CN", theme,
      local: true, hostActive: true, hostVisible: true, activeSessionId: session.id, selectedAgentId: session.agent_id,
      agents: [{ agent: { id: "guide", name: "Guide" } as AgentGuiInitSnapshot["agents"][number]["agent"], provider: "codex", iconUrl: avatar, canInvoke: true, runtimeBound: true }],
      sessions: [session], members: [{ userId: "user-a", name: "Alex", iconUrl: avatar }, { userId: "user-b", name: "Alex", iconUrl: avatar }],
      executionTargets: [{ id: "local:codex", name: "Codex", provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }],
    };
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.route("**/__test/member-mentions", route => route.fulfill({ contentType: "text/html", body: '<iframe title="Member test" style="width:100%;height:850px;border:0"></iframe>' }));
    await page.goto("/__test/member-mentions");
    await page.evaluate(({ snapshot, session, at }) => {
      const frame = document.querySelector("iframe")!;
      const storage = new Map<string, string>();
      let content = "";
      window.addEventListener("message", event => {
        if (event.source !== frame.contentWindow || event.origin !== location.origin || event.data?.type !== "multica:agent-gui:ready") return;
        const channel = new MessageChannel();
        channel.port1.onmessage = event => {
          const request = event.data;
          if (request.type !== "request") return;
          let result: unknown;
          switch (request.method) {
            case "workspace.load": result = snapshot; break;
            case "session.load": result = { session, messages: content ? [{ id: "message", chat_session_id: session.id, task_id: "turn", role: "user", content, created_at: at }] : [], tasks: content ? [{ id: "turn", agent_id: session.agent_id, chat_session_id: session.id, status: "completed", kind: "chat", created_at: at, completed_at: at }] : [], pending: {} }; break;
            case "queue.enqueue":
              content = request.params.item.content;
              document.body.dataset.sent = JSON.stringify(request.params.item);
              result = { revision: 1, paused: false, items: [request.params.item] };
              break;
            case "task.messages": case "context.tasks": result = []; break;
            case "storage.read": result = storage.get(request.params.key) ?? null; break;
            case "storage.write": storage.set(request.params.key, request.params.value); break;
            case "models.list": result = { id: "models", runtime_id: session.agent_id, status: "completed", supported: true, models: [] }; break;
            case "usage.read": result = { provider: "codex", context: { usedTokens: null, limitTokens: null, model: null, updatedAt: null }, account: { status: "unavailable", windows: [] } }; break;
            case "host.selectSession": case "session.watch": case "session.markRead": case "host.promptApplied": break;
            default: channel.port1.postMessage({ type: "response", id: request.id, error: { message: `Unexpected ${request.method}` } }); return;
          }
          channel.port1.postMessage({ type: "response", id: request.id, result });
        };
        channel.port1.start();
        frame.contentWindow!.postMessage({ type: "multica:agent-gui:connect", snapshot }, location.origin, [channel.port2]);
      });
      frame.src = "/tutti-agent-gui/index.html";
    }, { snapshot, session, at });
    const gui = page.frameLocator("iframe");
    const editor = gui.getByTestId("agent-gui-composer-editor");
    await expect(editor).toBeVisible();
    await editor.fill("@");
    await gui.getByRole("tab", { name: "智能体", exact: true }).click();
    const agentOption = gui.getByRole("option").filter({ hasText: "Guide" });
    await expect(agentOption.locator("img")).toBeVisible();
    const agentRowStyle = await agentOption.evaluate(mentionStyle);
    await page.screenshot({ path: `.tmp/member-mentions/agent-picker-${theme}.png` });
    await agentOption.click();
    const agentChip = editor.locator('[data-agent-mention-kind="agent-target"][data-agent-mention-href]');
    await expect(agentChip.locator("img")).toBeVisible();
    const agentChipStyle = await agentChip.evaluate(mentionStyle);
    await editor.fill("@");
    await gui.getByRole("tab", { name: "成员", exact: true }).click();
    const options = gui.getByRole("option").filter({ hasText: "Alex" });
    await expect(options).toHaveCount(2);
    await expect(options.nth(1)).not.toContainText("user-b");
    await expect(options.nth(1).locator("img")).toBeVisible();
    expect(await options.nth(1).evaluate(mentionStyle)).toEqual(agentRowStyle);
    await page.screenshot({ path: `.tmp/member-mentions/picker-${theme}.png` });
    await options.nth(1).click();
    const chip = editor.locator('[data-agent-mention-kind="workspace-member"][data-agent-mention-href]');
    await expect(chip).toContainText("Alex");
    await expect(chip.locator("img")).toBeVisible();
    expect(await chip.evaluate(mentionStyle)).toEqual(agentChipStyle);
    await expect(chip).toHaveAttribute("data-agent-mention-href", /workspace-member\/user-b\?/);
    await expect(page.locator("body")).not.toHaveAttribute("data-sent");
    await page.screenshot({ path: `.tmp/member-mentions/composer-${theme}.png` });
    await editor.press("End");
    await editor.pressSequentially(" 请把任务分配给他");
    await editor.press("Enter");
    await expect.poll(() => page.locator("body").getAttribute("data-sent")).not.toBeNull();
    const sent = JSON.parse((await page.locator("body").getAttribute("data-sent"))!);
    expect(sent.references).toEqual([{ kind: "member", id: "user-b", workspaceId: session.workspace_id }]);
    expect(sent.content).toContain("userId=user-b");
    expect(sent.content).not.toMatch(/iconUrl|data:image|role=|email=/);
    await expect(editor.locator('[data-agent-mention-kind="workspace-member"]')).toHaveCount(0);
    await page.evaluate(() => {
      const frame = document.querySelector("iframe")!;
      frame.src = frame.src;
    });
    const messageMention = gui.locator('.workspace-agents-status-panel__detail-user-message [data-agent-mention-kind="workspace-member"][data-agent-mention-href]');
    await expect(messageMention).toBeVisible();
    try {
      await expect(messageMention.locator("img")).toBeVisible();
    } finally {
      await messageMention.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
      await page.screenshot({ path: `.tmp/member-mentions/message-${theme}.png` });
    }
    await expect(messageMention).not.toContainText("user-b");
    expect(await messageMention.evaluate(mentionStyle)).toEqual(agentChipStyle);
    await expect(messageMention).toHaveAttribute("data-agent-mention-href", /workspace-member\/user-b\?/);
    expect(errors).toEqual([]);
  });
}
