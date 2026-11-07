// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { CuaComputer, translateKey, type ToolOutput } from "./driver";

const ok = (structured: Record<string, unknown>, images: ToolOutput["images"] = []): ToolOutput => ({ text: "", images, structured, isError: false });
const image = (bytes: number) => ({ mimeType: "image/jpeg", dataBase64: "A".repeat(Math.ceil(bytes * 4 / 3)) });
const windows = [
  { window_id: 7, pid: 42, title: "Back", layer: 0, z_index: 0, is_on_screen: true, on_current_space: true },
  { window_id: 8, pid: 42, title: "Front", layer: 0, z_index: 3, is_on_screen: true, on_current_space: true },
  { window_id: 9, pid: 42, title: "Elsewhere", layer: 0, z_index: 9, is_on_screen: false, on_current_space: false },
];
const state = (snapshot = "s00000001") => ok({
  snapshot_id: snapshot, window_bounds: { x: 100, y: 50, width: 400, height: 300 }, screenshot_width: 800, screenshot_height: 600,
  elements: [{ element_token: `${snapshot}:1`, role: "AXButton", label: "Save", enabled: true, depth: 2, frame: { x: 110, y: 60, w: 40, h: 20 }, actions: ["AXPress"] }],
}, [image(1000)]);

function fixture(overrides: Record<string, (args: Record<string, unknown>) => ToolOutput> = {}) {
  const calls: [string, Record<string, unknown>][] = [];
  const tools: Record<string, (args: Record<string, unknown>) => ToolOutput> = {
    list_apps: () => ok({ apps: [{ bundle_id: "com.example.editor", name: "Editor", running: true }, { bundle_id: "com.apple.Terminal", name: "Terminal" }, { bundle_id: "ai.multica.desktop", name: "Multica" }] }),
    list_windows: () => ok({ windows }),
    get_window_state: () => state(),
    ...overrides,
  };
  const callTool = vi.fn(async (name: string, args: Record<string, unknown>) => {
    calls.push([name, args]);
    return tools[name]?.(args) ?? ok({ effect: "confirmed", route: "accessibility" });
  });
  return { computer: new CuaComputer(callTool, 1), calls };
}
async function observed(overrides?: Parameters<typeof fixture>[0]) {
  const f = fixture(overrides);
  await f.computer.perform({ op: "get_app", app: "Editor" });
  const observation = await f.computer.perform({ op: "observe", app: "com.example.editor" }) as { observationId: string };
  return { ...f, observation };
}

describe("cua-driver Computer Use translation", () => {
  it("resolves apps by bundle ID or name and blocks protected apps but not Multica", async () => {
    const { computer } = fixture();
    await expect(computer.perform({ op: "get_app", app: "editor" })).resolves.toEqual({ id: "com.example.editor", name: "Editor" });
    await expect(computer.perform({ op: "get_app", app: "Terminal" })).rejects.toThrow("app_forbidden");
    await expect(computer.perform({ op: "get_app", app: "Multica" })).resolves.toEqual({ id: "ai.multica.desktop", name: "Multica" });
    await expect(computer.perform({ op: "observe", app: "com.other.app" })).rejects.toThrow("app_not_found");
  });

  it("observes the frontmost visible window with window-relative element bounds", async () => {
    const { calls, observation } = await observed();
    expect(calls.find(([name]) => name === "get_window_state")?.[1]).toMatchObject({ pid: 42, window_id: 8, screenshot_format: "jpeg" });
    expect(observation).toMatchObject({
      observationId: "s00000001", coordinateSpace: "window-logical-points", screenshotSize: { width: 800, height: 600 },
      elements: [{ ref: "s00000001:1", role: "AXButton", title: "Save", bounds: { x: 10, y: 10, width: 40, height: 20 } }],
      image: { mimeType: "image/jpeg" },
    });
  });

  it("lowers JPEG quality until the screenshot fits the transport budget", async () => {
    const qualities: unknown[] = [];
    const { observation } = await observed({ get_window_state: (args) => { qualities.push(args.screenshot_quality); return qualities.length < 3 ? ok({}, [image(600 * 1024)]) : state(); } });
    expect(qualities).toEqual([70, 45, 25]);
    expect(observation.observationId).toBe("s00000001");
  });

  it("targets a selected window and rejects unknown ones", async () => {
    const { computer, calls } = fixture();
    await computer.perform({ op: "get_app", app: "Editor" });
    await expect(computer.perform({ op: "select_window", app: "com.example.editor", windowId: "42:99" })).rejects.toThrow("window_unavailable");
    await computer.perform({ op: "select_window", app: "com.example.editor", windowId: "42:7" });
    await computer.perform({ op: "observe", app: "com.example.editor" });
    expect(calls.filter(([name]) => name === "get_window_state").at(-1)?.[1]).toMatchObject({ window_id: 7 });
  });

  it("converts logical points to screenshot pixels and consumes the observation", async () => {
    const { computer, calls, observation } = await observed();
    await computer.perform({ op: "click", app: "com.example.editor", observationId: observation.observationId, x: 10, y: 20 });
    expect(calls.at(-1)).toEqual(["click", { pid: 42, window_id: 8, x: 20, y: 40 }]);
    await expect(computer.perform({ op: "click", app: "com.example.editor", observationId: observation.observationId, ref: "s00000001:1" })).rejects.toThrow("stale_observation");
  });

  it("acts on refs only from the latest observation", async () => {
    const { computer, calls, observation } = await observed();
    await expect(computer.perform({ op: "set_value", app: "com.example.editor", observationId: observation.observationId, ref: "s00000099:1", value: "x" })).rejects.toThrow("stale_control");
    await computer.perform({ op: "observe", app: "com.example.editor" });
    await computer.perform({ op: "set_value", app: "com.example.editor", observationId: "s00000001", ref: "s00000001:1", value: "x" });
    expect(calls.at(-1)).toEqual(["set_value", { element_token: "s00000001:1", value: "x" }]);
  });

  it("maps keys, chords and scroll deltas onto cua-driver tools", async () => {
    const { computer, calls, observation } = await observed();
    await computer.perform({ op: "press", app: "com.example.editor", observationId: observation.observationId, key: "Meta+A" });
    expect(calls.at(-1)).toEqual(["hotkey", { pid: 42, window_id: 8, keys: ["cmd", "a"] }]);
    await computer.perform({ op: "observe", app: "com.example.editor" });
    await computer.perform({ op: "scroll", app: "com.example.editor", observationId: "s00000001", x: 10, y: 10, dy: -200 });
    expect(calls.at(-1)).toEqual(["scroll", { pid: 42, window_id: 8, x: 20, y: 20, direction: "up", amount: 5 }]);
    expect(translateKey("Enter")).toEqual({ key: "return", modifiers: [] });
    expect(() => translateKey("Hyper+A")).toThrow("invalid_modifier");
    expect(() => translateKey("PrintScreen")).toThrow("unsupported_key");
  });

  it("surfaces background refusals with their code and hint", async () => {
    const { computer, observation } = await observed({ type_text: () => ({ text: "", images: [], structured: { effect: "refused", error: { code: "background_unavailable", hint: "Electron web content refuses background typing" } }, isError: true }) });
    await expect(computer.perform({ op: "type", app: "com.example.editor", observationId: observation.observationId, text: "hi" })).rejects.toThrow("background_unavailable: Electron web content refuses background typing");
  });

  it("never exposes windows of the Multica process hosting Computer Use", async () => {
    const host = { window_id: 5, pid: 1, title: "Host", layer: 0, z_index: 99, is_on_screen: true, on_current_space: true };
    const { computer, calls } = fixture({ list_windows: () => ok({ windows: [...windows, host] }) });
    await computer.perform({ op: "get_app", app: "Editor" });
    const listed = await computer.perform({ op: "windows", app: "com.example.editor" }) as { windows: { id: string }[] };
    expect(listed.windows.map((w) => w.id)).not.toContain("1:5");
    await expect(computer.perform({ op: "select_window", app: "com.example.editor", windowId: "1:5" })).rejects.toThrow("window_unavailable");
    await computer.perform({ op: "observe", app: "com.example.editor" });
    expect(calls.find(([name]) => name === "get_window_state")?.[1]).toMatchObject({ pid: 42, window_id: 8 });
  });

  it("does not relaunch a running app", async () => {
    const { computer, calls } = fixture();
    await computer.perform({ op: "get_app", app: "Editor" });
    await computer.perform({ op: "launch", app: "com.example.editor" });
    expect(calls.some(([name]) => name === "launch_app")).toBe(false);
  });

  it("forgets an app's window and observation so the next turn starts fresh", async () => {
    const { computer, calls, observation } = await observed();
    await computer.perform({ op: "select_window", app: "com.example.editor", windowId: "42:7" });
    await computer.perform({ op: "forget", apps: ["com.example.editor"] });
    await expect(computer.perform({ op: "click", app: "com.example.editor", observationId: observation.observationId, x: 1, y: 1 })).rejects.toThrow("app_not_found");
    await computer.perform({ op: "get_app", app: "Editor" });
    await computer.perform({ op: "observe", app: "com.example.editor" });
    expect(calls.filter(([name]) => name === "get_window_state").at(-1)?.[1]).toMatchObject({ window_id: 8 });
  });
});
