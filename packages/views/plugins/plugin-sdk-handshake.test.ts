// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

async function loadSdk(port: MessagePort | null) {
  vi.resetModules();
  if (port) vi.stubGlobal("__multicaPluginBridgePortV2", port);
  return import("@multica/plugin-sdk");
}

describe("surface SDK guest port", () => {
  beforeEach(() => vi.unstubAllGlobals());

  it("takes the guest-created bootstrap port and answers on it", async () => {
    const channel = new MessageChannel();
    const asked: unknown[] = [];
    channel.port2.onmessage = (event) => {
      asked.push(event.data);
      const request = event.data as { id: string };
      channel.port2.postMessage({ id: request.id, ok: true, status: 200, data: { workspace: {}, user: {}, config: {} } });
    };
    channel.port2.start();

    const { multica } = await loadSdk(channel.port1);
    await multica.context.get(true);

    expect(asked).toHaveLength(1);
    expect(asked[0]).toMatchObject({ kind: "action", method: "GET", path: "/context" });
    expect((globalThis as Record<string, unknown>).__multicaPluginBridgePortV2).toBeUndefined();
  });

  it("applies theme events delivered before a plugin makes its first request", async () => {
    const style = { setProperty: vi.fn() };
    vi.stubGlobal("document", { documentElement: { style } });
    const channel = new MessageChannel();
    const { multica } = await loadSdk(channel.port1);
    channel.port2.postMessage({ kind: "theme", theme: { "--background": "black" } });

    await vi.waitFor(() => expect(style.setProperty).toHaveBeenCalledWith("--background", "black"));
    expect(typeof multica.ui.onThemeChange).toBe("function");
  });
});
