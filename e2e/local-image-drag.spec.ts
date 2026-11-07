import { expect, test } from "@playwright/test";
import type { AgentGuiInitSnapshot } from "../packages/tutti-agent-gui-host/src/protocol";

for (const theme of ["light", "dark"] as const) {
  test(`local conversation images disable native dragging (${theme})`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1200, height: 900 });
    await page.route("**/__test/image-drag", (route) => route.fulfill({
      contentType: "text/html",
      body: '<iframe title="Local conversation" style="display:block;width:100%;height:870px;border:0"></iframe>',
    }));
    await page.route("https://example.test/image-drag.png", async (route) => route.fulfill({
      contentType: "image/png", body: Buffer.from(await page.evaluate(() => document.body.dataset.image!), "base64"),
    }));
    await page.goto("/__test/image-drag");
    await page.evaluate((theme) => {
      const frame = document.querySelector("iframe")!;
      const at = "2026-11-06T10:00:00Z";
      const canvas = document.createElement("canvas");
      canvas.width = 300; canvas.height = 180;
      const context = canvas.getContext("2d")!;
      context.fillStyle = "#7799bb"; context.fillRect(0, 0, 300, 180);
      const image = canvas.toDataURL("image/png");
      document.body.dataset.image = image.split(",")[1]!;
      const session = { id: "images", workspace_id: "test", creator_id: "test", agent_id: "local:claude", title: "Image dragging", status: "active" as const, created_at: at, updated_at: at };
      const snapshot: AgentGuiInitSnapshot = {
        workspaceId: "test", workspaceSlug: "test", userId: "test", locale: "en", theme, local: true,
        hostActive: true, hostVisible: true, agents: [], sessions: [session], activeSessionId: session.id, selectedAgentId: session.agent_id,
        executionTargets: [{ id: session.agent_id, provider: "claude", name: "Claude Code", iconUrl: image, canInvoke: true, runtimeBound: true, runtimeOnline: true }],
      };
      let updated = false;
      window.addEventListener("message", (event) => {
        if (event.source !== frame.contentWindow || event.data?.type !== "multica:agent-gui:ready") return;
        const channel = new MessageChannel();
        const port = channel.port1;
        window.addEventListener("test-add-image", () => {
          updated = true;
          port.postMessage({ type: "event", event: { type: "local.reload", sessionId: session.id } });
        });
        port.onmessage = (event) => {
          const request = event.data;
          if (request.type !== "request") return;
          let result: unknown;
          switch (request.method) {
            case "workspace.load": result = snapshot; break;
            case "session.load": result = {
              session, pending: {}, tasks: [{ id: "turn", chat_session_id: session.id, status: "completed", created_at: at }],
              messages: [
                { id: "user", task_id: "turn", chat_session_id: session.id, role: "user", content: "Review these images", created_at: at },
                { id: "reply", task_id: "turn", chat_session_id: session.id, role: "assistant", content: `![Chart](https://example.test/image-drag.png)${updated ? `\n\n![New chart](https://example.test/image-drag.png)` : ""}`, created_at: at },
              ],
            }; break;
            case "storage.read": result = null; break;
            case "context.tasks": case "task.messages": result = []; break;
            case "models.list": result = { id: "models", status: "completed", supported: true, models: [] }; break;
            case "usage.read": result = { provider: "claude", context: { usedTokens: null, limitTokens: null, model: null, updatedAt: null }, account: { status: "unavailable", plan: null, windows: [], updatedAt: null } }; break;
          }
          port.postMessage({ type: "response", id: request.id, result });
        };
        port.start();
        frame.contentWindow!.postMessage({ type: "multica:agent-gui:connect", snapshot }, location.origin, [channel.port2]);
      });
      frame.src = "/tutti-agent-gui/index.html";
    }, theme);
    const gui = page.frameLocator("iframe");
    const chart = gui.getByTestId("agent-gui-timeline").getByRole("img", { name: "Chart", exact: true });
    await expect(chart).toBeVisible();
    await expect.poll(() => chart.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth)).toBe(300);
    await page.screenshot({ path: testInfo.outputPath(`images-${theme}.png`) });
    await expect(chart).toHaveAttribute("draggable", "false");
    await chart.evaluate((image) => {
      image.ownerDocument.documentElement.dataset.imageDrags = "0";
      image.ownerDocument.addEventListener("dragstart", () => {
        image.ownerDocument.documentElement.dataset.imageDrags = "1";
      });
    });
    const box = (await chart.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 80, box.y + box.height / 2 + 40, { steps: 12 });
    await page.mouse.up();
    expect(await chart.evaluate((image) => image.ownerDocument.documentElement.dataset.imageDrags)).toBe("0");

    if (await gui.getByRole("dialog").isVisible()) await gui.getByRole("dialog").press("Escape");
    await page.evaluate(() => window.dispatchEvent(new Event("test-add-image")));
    await expect(gui.getByRole("img", { name: "New chart", exact: true })).toBeVisible();
    await chart.locator("..").getByRole("button", { name: "Zoom image", exact: true }).click();
    const dialog = gui.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.locator("img")).toHaveAttribute("draggable", "false");
    await dialog.press("Escape");
    await expect(dialog).toBeHidden();
    await gui.getByTestId("agent-gui-composer-editor").evaluate((editor) => {
      const canvas = editor.ownerDocument.createElement("canvas");
      canvas.width = canvas.height = 16;
      const data = new DataTransfer();
      const bytes = Uint8Array.from(atob(canvas.toDataURL("image/png").split(",")[1]!), (value) => value.charCodeAt(0));
      data.items.add(new File([bytes], "draft.png", { type: "image/png" }));
      editor.dispatchEvent(new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: data }));
    });
    await expect(gui.getByTestId("agent-gui-composer-image-draft").locator("img")).toBeVisible();
    expect(await gui.locator("img").evaluateAll((images) => images.filter((image) => image.getAttribute("draggable") !== "false").map((image) => image.outerHTML))).toEqual([]);
  });
}
