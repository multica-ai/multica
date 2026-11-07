import "./env";
import { expect, test, type Page } from "@playwright/test";
import type { AgentGuiInitSnapshot } from "../packages/tutti-agent-gui-host/src/protocol";
import type { AgentTask, ChatMessage, ChatSession } from "../packages/core/types";
import type { LocalPromptQueue } from "../packages/core/local-conversation/protocol";

async function mountQueueHarness(page: Page, snapshot: AgentGuiInitSnapshot, session: ChatSession, provider: "codex" | "claude", seed?: string) {
  // All local submissions now enter the desktop-owned queue, including the first turn.
  snapshot = { ...snapshot, queueOwner: "desktop" };
  await page.route("**/__test/local-conversation-queue", (route) => route.fulfill({ contentType: "text/html", body: '<button id="complete">Complete next execution away from composer</button><iframe title="Queue test" style="display:block;width:100%;height:850px;border:0"></iframe>' }));
  await page.setViewportSize({ width: 1200, height: 950 });
  await page.goto("/__test/local-conversation-queue");
  await page.evaluate(({ snapshot, session, provider, seed }) => {
    const frame = document.querySelector("iframe")!;
    const previous = JSON.parse(localStorage.getItem("test-queue-backend") || "null") as { messages: ChatMessage[]; tasks: AgentTask[]; sends: { content: string; queued?: true }[]; queue?: LocalPromptQueue } | null;
    const messages = previous?.messages ?? (seed ? [
      { id: "input-1", chat_session_id: session.id, role: "user", content: seed, task_id: "turn-1", created_at: session.created_at },
      { id: "reply-1", chat_session_id: session.id, role: "assistant", content: `Reply ${seed}`, task_id: "turn-1", created_at: session.updated_at }
    ] as ChatMessage[] : []);
    const tasks = previous?.tasks ?? (seed ? [{ id: "turn-1", chat_session_id: session.id, status: "completed", created_at: session.created_at, completed_at: session.updated_at }] as AgentTask[] : []);
    const sends = previous?.sends ?? (seed ? [{ content: seed }] : []);
    // A stopped desktop marks interrupted local executions terminal and pauses its queue on reopen.
    const interrupted = tasks.some((task) => task.status === "running");
    for (const task of tasks) if (task.status === "running") { task.status = "failed"; task.completed_at = new Date(Date.parse(session.created_at) + tasks.length * 1000).toISOString(); session.updated_at = task.completed_at; }
    const queue: LocalPromptQueue = previous?.queue ?? { revision: 0, paused: false, items: [] };
    if (interrupted) { queue.paused = true; queue.revision++; }
    const save = () => localStorage.setItem("test-queue-backend", JSON.stringify({ messages, tasks, sends, queue }));
    document.body.dataset.sends = JSON.stringify(sends);
    let port: MessagePort;
    const drainDesktop = () => {
      if (snapshot.queueOwner !== "desktop" || queue.paused || tasks.some((task) => task.status === "running")) return;
      const item = queue.items[0]; if (!item) return;
      if (item.content === "Queue failure fixture") {
        queue.error = "Queue send failed. Check the connection and retry the queued message.";
        queue.paused = true;
        queue.revision++;
        document.body.dataset.failure = queue.error;
        return;
      }
      queue.items.shift();
      sends.push({ content: item.content, queued: true }); queue.revision++;
      const id = `turn-${sends.length}`;
      messages.push({ id: `input-${sends.length}`, chat_session_id: session.id, role: "user", content: item.content, task_id: id, created_at: session.updated_at } as ChatMessage);
      tasks.push({ id, chat_session_id: session.id, status: "running", created_at: session.updated_at, started_at: session.updated_at } as AgentTask);
      document.body.dataset.sends = JSON.stringify(sends); save();
    };
    document.querySelector("#complete")!.addEventListener("click", () => {
      const task = tasks.find((entry) => entry.status === "running");
      if (!task) throw new Error("Expected a running execution");
      const completedAt = new Date(Date.parse(session.created_at) + tasks.length * 1000).toISOString();
      task.status = "completed"; task.completed_at = completedAt; session.updated_at = completedAt;
      messages.push({ id: `reply-${tasks.length}`, chat_session_id: session.id, role: "assistant", content: `Reply ${sends.at(-1)!.content}`, task_id: task.id, created_at: completedAt } as ChatMessage);
      drainDesktop();
      save();
      port.postMessage({ type: "event", event: { type: "invalidate", scope: "session", sessionId: session.id } });
      port.postMessage({ type: "event", event: { type: "invalidate", scope: "workspace" } });
    });
    window.addEventListener("message", (event) => {
      if (event.source !== frame.contentWindow || event.origin !== location.origin || event.data?.type !== "multica:agent-gui:ready") return;
      const channel = new MessageChannel(); port = channel.port1;
      port.onmessage = (event) => {
        const request = event.data;
        if (request.type !== "request") return;
        const fail = (message: string, code?: string) => {
          document.body.dataset.failure = message;
          port.postMessage({ type: "response", id: request.id, error: { message, code, status: 409 } });
        };
        let result: unknown;
        const running = tasks.find((entry) => entry.status === "running");
        switch (request.method) {
          case "queue.enqueue": snapshot.activeSessionId = request.params.sessionId; queue.items.push(request.params.item); queue.paused = request.params.paused === true; queue.revision++; drainDesktop(); save(); result = queue; break;
          case "queue.update": {
            if (request.params.action === "remove") queue.items = queue.items.filter((item) => item.requestId !== request.params.requestId);
            else queue.paused = request.params.action === "pause" || request.params.action === "stop";
            queue.revision++; drainDesktop(); save(); result = queue; break;
          }
          case "host.openSide": {
            document.body.dataset.sideRequest = JSON.stringify(request.params);
            // The host transfers the queued request and starts Side atomically.
            if (request.params.queued) {
              const index = queue.items.findIndex((item) => item.requestId === request.params.queuedRequestId);
              if (index < 0) { fail("Queued request no longer exists"); return; }
              document.body.dataset.sideQueuedContent = queue.items.splice(index, 1)[0].content;
              document.body.dataset.sideStarted = "side-session";
              queue.revision++;
              save();
            }
            result = { ...session, id: "side-session", title: "Side" }; break;
          }
          case "host.startSide": document.body.dataset.sideStarted = request.params.sessionId; break;
          case "workspace.load": result = { ...snapshot, pendingTasks: running ? [{ task_id: running.id, status: "running", chat_session_id: session.id }] : [] }; break;
          case "session.create": session.id = request.params.sessionId; snapshot.sessions = [session]; result = session; break;
          case "session.load": result = { session, messages, tasks, promptQueue: queue, pending: running ? { task_id: running.id, status: "running", created_at: running.created_at } : {} }; break;
          case "session.send": {
            fail("Local submissions must use queue.enqueue"); return;
          }
          case "storage.read": result = localStorage.getItem(`test-queue:${request.params.key}`); break;
          case "storage.write": localStorage.setItem(`test-queue:${request.params.key}`, request.params.value); break;
          case "task.messages": case "context.tasks": result = []; break;
          case "models.list": result = { id: "queue-models", status: "completed", supported: true, models: [] }; break;
          case "usage.read": result = { provider, context: { usedTokens: null, limitTokens: null, model: null, updatedAt: null }, account: { status: "unavailable", plan: null, windows: [], updatedAt: null } }; break;
          case "host.selectSession": if (request.params.sessionId) snapshot.activeSessionId = request.params.sessionId; break;
          case "session.watch": case "session.markRead": case "host.promptApplied": break;
          case "host.showMessage": fail(request.params.message); return;
          default: fail(`Unexpected request: ${request.method}`); return;
        }
        port.postMessage({ type: "response", id: request.id, result });
        if (request.method === "queue.enqueue") port.postMessage({ type: "event", event: { type: "invalidate", scope: "session", sessionId: session.id } });
      };
      port.start();
      frame.contentWindow!.postMessage({ type: "multica:agent-gui:connect", snapshot }, location.origin, [channel.port2]);
    });
    frame.src = "/tutti-agent-gui/index.html";
  }, { snapshot, session, provider, seed });
}

for (const theme of ["light", "dark"] as const) test(`queue errors stay above the queue and growing input in ${theme}`, async ({ page }) => {
  const at = new Date().toISOString();
  const session = { id: "queue-error", workspace_id: "queue-workspace", creator_id: "queue-user", agent_id: "local:claude", title: "Queue error", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
  const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale: "en", theme, local: true, hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
    executionTargets: [{ id: session.agent_id, provider: "claude", name: "Claude Code", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }] };
  await mountQueueHarness(page, snapshot, session, "claude");
  const gui = page.frameLocator("iframe");
  const editor = gui.getByTestId("agent-gui-composer-editor");
  const send = gui.getByRole("button", { name: "Send", exact: true });
  await editor.fill("Original request"); await send.click();
  for (const prompt of ["Queue failure fixture", "Second queued request", "Third queued request"]) { await editor.fill(prompt); await send.click(); }
  await page.getByRole("button", { name: "Complete next execution away from composer" }).click();
  const error = gui.getByRole("alert").filter({ hasText: "Queue send failed" });
  const queue = gui.getByTestId("agent-gui-composer-queued-prompts");
  await expect(error).toBeVisible(); await expect(queue).toContainText("Queue failure fixture");
  for (const width of [1200, 380]) {
    await page.setViewportSize({ width, height: 950 });
    for (const draft of ["", "A multi-line draft\nSecond line\nThird line\nFourth line"]) {
      await editor.fill(draft);
      for (const expanded of [false, true]) {
        const panel = queue.locator(".agent-gui-node__composer-queued-prompt-panel");
        if ((await panel.getAttribute("data-expanded")) !== String(expanded)) await panel.locator(".agent-gui-node__composer-queued-prompt-header").click();
        await expect.poll(async () => {
          const errorBox = (await error.boundingBox())!;
          const queueBox = (await panel.boundingBox())!;
          const inputBox = (await gui.getByTestId("agent-gui-composer-input-shell").boundingBox())!;
          return { errorAboveQueue: errorBox.y + errorBox.height + 7 <= queueBox.y, queueAboveInput: queueBox.y + queueBox.height <= inputBox.y + 1 };
        }).toEqual({ errorAboveQueue: true, queueAboveInput: true });
      }
    }
    await page.screenshot({ path: `.tmp/queue-error-layout/${theme}-${width}.png` });
  }
  await expect(page.locator("body")).toHaveAttribute("data-failure", "Queue send failed. Check the connection and retry the queued message.");
});

for (const provider of ["codex", "claude"] as const) for (const fresh of [false, true]) {
  test(`desktop-owned ${provider} queue continues while the GUI document is destroyed (fresh: ${fresh})`, async ({ page }) => {
    const at = "2026-10-25T10:00:00Z";
    const session = { id: "desktop-queue", workspace_id: "queue-workspace", creator_id: "queue-user", agent_id: `local:${provider}`, title: "Desktop queue", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
    const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale: "en", theme: "dark", local: true, queueOwner: "desktop", hostActive: true, hostVisible: true, agents: [], sessions: fresh ? [] : [session], activeSessionId: fresh ? null : session.id, selectedAgentId: session.agent_id,
      executionTargets: [{ id: session.agent_id, provider, name: provider === "codex" ? "Codex" : "Claude Code", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }] };
    await mountQueueHarness(page, snapshot, session, provider);
    const gui = page.frameLocator("iframe");
    const editor = gui.getByTestId("agent-gui-composer-editor");
    await editor.fill("First native turn"); await gui.getByRole("button", { name: "Send", exact: true }).click();
    await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
    await editor.fill("Continue without this view"); await gui.getByRole("button", { name: "Send", exact: true }).click();
    await expect(gui.getByTestId("agent-gui-composer-queued-prompts")).toContainText("Continue without this view");
    await page.locator("iframe").evaluate((frame: HTMLIFrameElement) => { frame.src = "about:blank"; });
    await page.locator("#complete").click();
    await expect.poll(() => page.locator("body").getAttribute("data-sends")).toContain("Continue without this view");
    await page.locator("iframe").evaluate((frame: HTMLIFrameElement) => { frame.src = "/tutti-agent-gui/index.html"; });
    await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
    await expect(gui.getByTestId("agent-gui-composer-queued-prompts")).not.toBeVisible();
    expect(await page.locator("body").getAttribute("data-failure")).toBeNull();
  });
}

for (const locale of ["zh-CN", "en"] as const) for (const theme of ["light", "dark"] as const) {
  test(`queued prompt menu aligns compact actions in ${locale} ${theme}`, async ({ page }) => {
    const at = "2026-10-24T10:00:00Z";
    const session = { id: "queue-menu", workspace_id: "queue-workspace", creator_id: "queue-user", agent_id: "local:codex", title: "Queue menu", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
    const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale, theme, local: true, sideWorkspace: true, hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
      executionTargets: [{ id: session.agent_id, provider: "codex", name: "Codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }] };
    await mountQueueHarness(page, snapshot, session, "codex");
    const gui = page.frameLocator("iframe");
    const editor = gui.getByTestId("agent-gui-composer-editor");
    const send = gui.getByRole("button", { name: locale === "en" ? "Send" : "发送", exact: true });
    await editor.fill("Original request"); await send.click();
    await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
    await editor.fill("Queued request"); await send.click();
    const more = gui.getByTestId("agent-gui-composer-queued-prompts").getByRole("button").last();
    for (const width of [1200, 380]) {
      await page.setViewportSize({ width, height: 950 });
      const alignment = await gui.getByTestId("agent-gui-composer-queued-prompts").evaluate((element) => {
        const panelElement = element.querySelector(".agent-gui-node__composer-queued-prompt-panel")!;
        const panel = panelElement.getBoundingClientRect();
        const input = document.querySelector('.agent-gui-node__composer[data-layout="dock"] .agent-gui-node__composer-input-shell')!.getBoundingClientRect();
        const text = panelElement.querySelector(".agent-gui-node__composer-queued-prompt-text")!.getBoundingClientRect();
        const editor = document.querySelector('[data-testid="agent-gui-composer-editor"]')!.getBoundingClientRect();
        return { left: panel.left - input.left, right: panel.right - input.right, textLeft: text.left - editor.left };
      });
      expect(Math.abs(alignment.left)).toBeLessThanOrEqual(1);
      expect(Math.abs(alignment.right)).toBeLessThanOrEqual(1);
      expect(Math.abs(alignment.textLeft)).toBeLessThanOrEqual(1);
      await more.click();
      const action = gui.getByRole("menuitem", { name: locale === "en" ? /Move to side and run/ : /移至旁支执行/ });
      await expect(action).toBeVisible();
      await expect(gui.getByRole("menu")).toHaveCSS("opacity", "1");
      const layout = await action.evaluate((element) => {
        const menu = element.closest('[role="menu"]')!;
        const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
        let title: Node | null = null;
        while (walker.nextNode()) if (walker.currentNode.textContent?.trim()) { title = walker.currentNode; break; }
        const range = document.createRange(); range.selectNodeContents(title!);
        const items = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')];
        const rows = items.map((item) => {
          const icon = item.querySelector("svg")!.getBoundingClientRect();
          const text = item.querySelector("span")!.getBoundingClientRect();
          return { height: item.getBoundingClientRect().height, iconLeft: icon.left, textLeft: text.left };
        });
        const bounds = menu.getBoundingClientRect();
        return { lines: range.getClientRects().length, rows, width: bounds.width, height: bounds.height, left: bounds.left, right: bounds.right, top: bounds.top, bottom: bounds.bottom, viewportWidth: innerWidth, viewportHeight: innerHeight };
      });
      expect(layout.width).toBeLessThan(224);
      expect(layout.lines).toBe(1);
      expect(layout.rows).toHaveLength(2);
      expect(Math.abs(layout.rows[0].height - layout.rows[1].height)).toBeLessThanOrEqual(1);
      expect(layout.rows[0].iconLeft).toBe(layout.rows[1].iconLeft);
      expect(layout.rows[0].textLeft).toBe(layout.rows[1].textLeft);
      expect(layout.height).toBeLessThanOrEqual(80);
      expect(layout.left).toBeGreaterThanOrEqual(0);
      expect(layout.right).toBeLessThanOrEqual(layout.viewportWidth);
      expect(layout.top).toBeGreaterThanOrEqual(0);
      expect(layout.bottom).toBeLessThanOrEqual(layout.viewportHeight);
      await page.screenshot({ path: `.tmp/local-conversations/queue-menu-${locale}-${theme}-${width}.png` });
      await page.keyboard.press("Escape");
      await expect(action).not.toBeVisible();
      await expect(more).toBeFocused();
    }
    await more.click();
    await page.keyboard.press("End");
    await expect(gui.getByRole("menuitem", { name: locale === "en" ? "Edit" : "编辑", exact: true })).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(editor).toHaveText("Queued request");
    await expect(gui.getByTestId("agent-gui-composer-queued-prompts")).not.toBeVisible();
    await expect(page.locator("body")).not.toHaveAttribute("data-side-request");
    await expect(page.locator("body")).not.toHaveAttribute("data-failure");
  });
}

for (const provider of ["codex", "claude"] as const) {
  const at = "2026-10-24T10:00:00Z";
  const session = { id: "side-source", workspace_id: "side-workspace", creator_id: "side-user", agent_id: `local:${provider}`, title: "Side source", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
  const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale: "en", theme: "light", local: true, sideWorkspace: true, hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
    executionTargets: [{ id: session.agent_id, provider, name: provider === "codex" ? "Codex" : "Claude Code", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }] };
  test(`${provider} moves a queued GUI prompt to Side without stopping the original run`, async ({ page }) => {
    await mountQueueHarness(page, snapshot, session, provider);
    const gui = page.frameLocator("iframe"); const editor = gui.locator('[contenteditable="true"]').first(); const send = gui.getByRole("button", { name: "Send", exact: true });
    await editor.fill("Original running request"); await send.click();
    await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
    await editor.fill("Move this queued request"); await send.click();
    const queue = gui.getByTestId("agent-gui-composer-queued-prompts");
    await expect(queue).toContainText("Move this queued request");
    await queue.getByRole("button", { name: "More queued prompt actions" }).click();
    await gui.getByRole("menuitem", { name: /Move to side and run/ }).click();
    await expect(page.locator("body")).toHaveAttribute("data-side-started", "side-session");
    const moved = await page.locator("body").evaluate((body) => JSON.parse(body.dataset.sideRequest || "{}"));
    expect(moved).toMatchObject({ sessionId: session.id, queued: true, queuedRequestId: expect.any(String) });
    await expect(page.locator("body")).toHaveAttribute("data-side-queued-content", "Move this queued request");
    await expect(queue).not.toBeVisible();
    await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
    expect(await page.locator("body").evaluate((body) => JSON.parse(body.dataset.sends || "[]").map((send: { content: string }) => send.content))).toEqual(["Original running request"]);
    await expect(page.locator("body")).not.toHaveAttribute("data-failure");
  });
  test(`${provider} opens Side from selected transcript text with its source turn`, async ({ page }) => {
    await mountQueueHarness(page, snapshot, session, provider);
    const gui = page.frameLocator("iframe"); const editor = gui.locator('[contenteditable="true"]').first();
    await editor.fill("2"); await gui.getByRole("button", { name: "Send", exact: true }).click();
    await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
    await page.getByRole("button", { name: "Complete next execution away from composer" }).click();
    await expect(gui.getByText("Reply 2", { exact: true })).toBeVisible();
    await gui.getByText("Reply 2", { exact: true }).evaluate((element) => { const range = document.createRange(); range.selectNodeContents(element); const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range); document.dispatchEvent(new PointerEvent("pointerup", { bubbles: true })); });
    await gui.getByRole("button", { name: "Ask in side", exact: true }).click();
    await expect.poll(() => page.locator("body").getAttribute("data-side-request")).not.toBeNull();
    const request = await page.locator("body").evaluate((body) => JSON.parse(body.dataset.sideRequest || "{}"));
    expect(request).toMatchObject({ sessionId: session.id, turnId: "turn-1", quote: "Reply 2" });
    await expect(page.locator("body")).not.toHaveAttribute("data-side-started");
    await expect(page.locator("body")).not.toHaveAttribute("data-failure");
  });
}

test("an empty saved Side keeps its composer without the floating home carousel", async ({ page }) => {
  const at = "2026-10-24T10:00:00Z";
  const session = { id: "empty-side", workspace_id: "side-workspace", creator_id: "side-user", agent_id: "local:codex", title: "Side", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
  const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale: "en", theme: "light", local: true, sideWorkspace: true, sidePane: true, hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
    executionTargets: ["codex", "claude"].map((provider) => ({ id: `local:${provider}`, provider, name: provider, iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true })) };
  await mountQueueHarness(page, snapshot, session, "codex");
  const gui = page.frameLocator("iframe");
  await expect(gui.locator(".multica-empty-conversation-welcome")).toHaveCount(1);
  await expect(gui.locator(".multica-empty-conversation-welcome")).not.toBeVisible();
  await expect(gui.locator(".agent-gui-node__empty-hero-carousel-layer")).not.toBeVisible();
  await expect(gui.getByTestId("agent-gui-composer-editor")).toBeVisible();
  await expect(gui.getByRole("button", { name: "Local provider", exact: true })).toBeDisabled();
  await expect(gui.getByRole("button", { name: "Model", exact: true })).toBeEnabled();
});

for (const theme of ["light", "dark"] as const) test(`selected-text actions use a compact Multica surface in ${theme}`, async ({ page }) => {
  const at = "2026-10-24T10:00:00Z";
  const session = { id: "selection-style", workspace_id: "style-workspace", creator_id: "style-user", agent_id: "local:codex", title: "Selection", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
  const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale: "zh-CN", theme, local: true, queueOwner: "desktop", sideWorkspace: true, hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
    executionTargets: [{ id: session.agent_id, provider: "codex", name: "Codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }] };
  await mountQueueHarness(page, snapshot, session, "codex", "2");
  await page.setViewportSize({ width: 380, height: 950 });
  const gui = page.frameLocator("iframe");
  const reply = gui.getByText("Reply 2", { exact: true });
  await expect(reply).toBeVisible();
  await reply.evaluate((element) => { const range = document.createRange(); range.selectNodeContents(element); const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range); document.dispatchEvent(new PointerEvent("pointerup", { bubbles: true })); });
  const toolbar = gui.getByRole("toolbar", { name: "选中文字操作" });
  await expect(toolbar).toBeVisible();
  const surface = await toolbar.evaluate((element) => {
    const style = getComputedStyle(element), rect = element.getBoundingClientRect();
    return { border: style.borderTopColor, foreground: style.color, height: rect.height, left: rect.left, right: rect.right, viewport: innerWidth };
  });
  expect(surface.border).not.toBe(surface.foreground);
  expect(surface.height).toBeLessThanOrEqual(34);
  expect(surface.left).toBeGreaterThanOrEqual(8);
  expect(surface.right).toBeLessThanOrEqual(surface.viewport - 8);
  await page.screenshot({ path: `.tmp/side-design/selection-toolbar-${theme}.png` });
  await reply.evaluate((element) => {
    const target = element as HTMLElement;
    target.style.position = "fixed"; target.style.top = "0"; target.style.right = "0";
    const range = document.createRange(); range.selectNodeContents(target); const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range); document.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
  });
  const edge = await toolbar.evaluate((element) => {
    const rect = element.getBoundingClientRect(), selected = window.getSelection()!.getRangeAt(0).getBoundingClientRect();
    return { top: rect.top, left: rect.left, right: rect.right, bottom: selected.bottom, viewport: innerWidth };
  });
  expect(edge.top).toBeGreaterThanOrEqual(edge.bottom + 8);
  await expect.poll(() => toolbar.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return rect.left >= 8 && rect.right <= innerWidth - 8;
  })).toBe(true);
  await gui.getByRole("button", { name: "在旁支中询问", exact: true }).press("Escape");
  await expect(toolbar).not.toBeVisible();
});

for (const theme of ["light", "dark"] as const) test(`selected quote matches the HTML interaction and reaches the current conversation in ${theme}`, async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const at = "2026-11-07T10:00:00Z";
  const session = { id: "quote-session", workspace_id: "quote-workspace", creator_id: "quote-user", agent_id: "local:claude", title: "引用到输入框", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
  const snapshot: AgentGuiInitSnapshot = { workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id, locale: "zh-CN", theme, local: true, queueOwner: "desktop", sideWorkspace: true, hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
    executionTargets: [{ id: session.agent_id, provider: "claude", name: "Claude Code", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }] };
  const original = Array.from({ length: 8 }, (_, i) => `第 ${i + 1} 行：Desktop 每次连上 daemon，第一条消息先发 hello。`).join("\n");
  await mountQueueHarness(page, snapshot, session, "claude", original);
  const gui = page.frameLocator("iframe");
  const reply = gui.locator('[data-agent-message-speaker="assistant"] p').first();
  const editor = gui.getByTestId("agent-gui-composer-editor");
  const card = gui.getByTestId("agent-gui-composer-quote-drafts");
  const quote = gui.getByTestId("agent-gui-composer-quote-text");
  const send = gui.getByRole("button", { name: "发送", exact: true });
  const select = async (short = false) => {
    await reply.evaluate((element, short) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      if (short) range.setEnd(element.firstChild!, 5);
      const selection = window.getSelection()!;
      selection.removeAllRanges(); selection.addRange(range);
      document.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    }, short);
  };
  await editor.fill("这段连接流程需要考虑哪些异常？");
  for (const width of [1440, 1024, 390]) {
    await page.setViewportSize({ width, height: 950 });
    await select();
    const toolbar = gui.getByRole("toolbar", { name: "选中文字操作" });
    await expect(toolbar.getByRole("button")).toHaveText(["复制", "添加到输入框", "在旁支中询问"]);
    await page.screenshot({ path: `.tmp/quote-design/toolbar-${theme}-${width}.png` });
    await gui.getByRole("button", { name: "添加到输入框", exact: true }).click();
    await expect(toolbar).not.toBeVisible();
    await expect(editor).toBeFocused();
    await expect.poll(() => editor.evaluate((element) => {
      const selection = getSelection();
      return Boolean(selection?.isCollapsed && element.contains(selection.anchorNode));
    })).toBe(true);
    await expect(editor).toHaveText("这段连接流程需要考虑哪些异常？");
    await expect(card).toContainText("引用 · Claude Code");
    await expect(quote).toHaveAttribute("data-expanded", "false");
    await expect(gui.getByRole("button", { name: "展开引用", exact: true })).toBeVisible();
    const layout = await quote.evaluate((element) => {
      const style = getComputedStyle(element);
      const rect = element.getBoundingClientRect();
      const card = element.closest('[data-testid="agent-gui-composer-quote-drafts"]')!.getBoundingClientRect();
      return { height: rect.height, twoLines: parseFloat(style.lineHeight) * 2, left: card.left, right: card.right, viewport: innerWidth, selectedText: getSelection()?.toString() };
    });
    expect(layout.height).toBeLessThanOrEqual(layout.twoLines + 1);
    expect(layout.left).toBeGreaterThanOrEqual(0);
    expect(layout.right).toBeLessThanOrEqual(layout.viewport);
    expect(layout.selectedText).toBe("");
    await expect.poll(() => card.evaluate((element) => {
      const input = element.closest<HTMLElement>(".agent-gui-node__composer-prompt-input-area")!;
      const inputBox = input.getBoundingClientRect();
      const quoteBox = element.getBoundingClientRect();
      const targetHeight = parseFloat(getComputedStyle(input).getPropertyValue("--agent-gui-composer-input-height"));
      return Math.abs(inputBox.height - targetHeight) < 1 && quoteBox.top >= inputBox.top && quoteBox.bottom <= inputBox.bottom;
    })).toBe(true);
    await page.screenshot({ path: `.tmp/quote-design/quote-${theme}-${width}.png` });
    await gui.getByRole("button", { name: "展开引用", exact: true }).click();
    await expect(quote).toHaveAttribute("data-expanded", "true");
    expect((await quote.boundingBox())!.height).toBeLessThanOrEqual(160);
    await gui.getByRole("button", { name: "收起引用", exact: true }).click();
    await select(true);
    await gui.getByRole("button", { name: "添加到输入框", exact: true }).click();
    await expect(card).toHaveCount(1);
    await expect(quote).toHaveText("Reply");
    await gui.getByRole("button", { name: "移除引用", exact: true }).click();
    await expect(card).not.toBeVisible();
    await expect(editor).toBeFocused();
    await expect(editor).toHaveText("这段连接流程需要考虑哪些异常？");
  }
  await editor.fill("");
  await select();
  await gui.getByRole("button", { name: "添加到输入框", exact: true }).click();
  await expect(send).toBeDisabled();
  const submittedQuote = await quote.textContent();
  await editor.press("Enter");
  expect(JSON.parse((await page.locator("body").getAttribute("data-sends"))!)).toHaveLength(1);
  await editor.fill("继续分析异常情况");
  await send.click();
  await expect.poll(async () => JSON.parse((await page.locator("body").getAttribute("data-sends"))!).length).toBe(2);
  const sends = JSON.parse((await page.locator("body").getAttribute("data-sends"))!) as { content: string }[];
  expect(sends[1]!.content).toBe(`> ${submittedQuote!.split("\n").join("\n> ")}\n\n继续分析异常情况`);
  await expect(gui.locator('[data-agent-message-speaker="user"] blockquote').last()).toContainText("第一条消息先发 hello");
  await expect(card).not.toBeVisible();
  await expect(editor).toBeEmpty();
  expect(await page.locator("body").getAttribute("data-side-request")).toBeNull();
  expect(errors).toEqual([]);
});

for (const provider of ["codex", "claude"] as const) for (const reopen of [false, true]) test(`${provider} ${reopen ? "restores a saved queue paused after closing the window" : "drains queued GUI messages after leaving the composer"}`, async ({ page, context }) => {
  const at = "2026-10-24T10:00:00Z";
  const session = { id: "queue-session", workspace_id: "queue-workspace", creator_id: "queue-user", agent_id: `local:${provider}`, title: "Queue regression", status: "active", created_at: at, updated_at: at, has_unread: false } as ChatSession;
  const snapshot: AgentGuiInitSnapshot = {
    workspaceId: session.workspace_id, workspaceSlug: "test", userId: session.creator_id,
    locale: "en", theme: "dark", local: true, hostActive: true, hostVisible: true,
    agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
    executionTargets: [{ id: session.agent_id, provider, name: provider === "codex" ? "Codex" : "Claude Code", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }],
  };
  await mountQueueHarness(page, snapshot, session, provider);
  let gui = page.frameLocator("iframe");
  const editor = gui.getByTestId("agent-gui-composer-editor");
  const send = gui.getByTestId("agent-gui-composer-send");
  await expect(editor).toBeVisible({ timeout: 30_000 });
  await editor.fill("2"); await send.click();
  await expect(gui.getByTestId("agent-gui-composer-stop-active-turn")).toBeVisible();
  for (const content of ["3", "4", "5", "6"]) { await editor.fill(content); await send.click(); }
  let queue = gui.getByTestId("agent-gui-composer-queued-prompts");
  await expect(queue).toBeVisible();
  const sent = () => page.locator("body").evaluate((body) => JSON.parse(body.dataset.sends || "[]") as { content: string; queued?: true }[]);
  expect(await sent()).toEqual([{ content: "2", queued: true }]);
  if (reopen) {
    await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("test-queue-backend") || "{}").queue?.items.length)).toBe(4);
    await page.close();
    page = await context.newPage();
    await mountQueueHarness(page, snapshot, session, provider);
    gui = page.frameLocator("iframe");
    queue = gui.getByTestId("agent-gui-composer-queued-prompts");
    await expect(queue).toContainText("Queue paused");
    expect(await sent()).toEqual([{ content: "2", queued: true }]);
    await queue.getByRole("button", { name: "Send next", exact: true }).first().click();
    await expect.poll(async () => (await sent()).length).toBe(2);
  }
  for (let count = reopen ? 3 : 2; count <= 5; count++) {
    await page.getByRole("button", { name: "Complete next execution away from composer" }).click();
    await expect.poll(async () => (await sent()).length).toBe(count);
  }
  await page.getByRole("button", { name: "Complete next execution away from composer" }).click();
  await expect(queue).not.toBeVisible();
  await expect(gui.getByText("Reply 6", { exact: true })).toBeVisible();
  expect(await sent()).toEqual(["2", "3", "4", "5", "6"].map((content) => ({ content, queued: true })));
  expect(await page.locator("body").getAttribute("data-failure")).toBeNull();
});
