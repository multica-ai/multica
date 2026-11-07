/**
 * Translates the broker's Computer Use operations onto cua-driver tools.
 *
 * cua-driver delivers input to the target process in the background, so the
 * user's frontmost app, key window and pointer stay put. Coordinates in this
 * contract are window-relative logical points; cua-driver takes screenshot
 * pixels, so every observation records the scale between the two.
 */

export interface ToolOutput {
  text: string;
  images: { mimeType: string; dataBase64: string }[];
  structured: Record<string, unknown> | null;
  isError: boolean;
  errorCode?: string;
}
export type CallTool = (name: string, args: Record<string, unknown>) => Promise<ToolOutput>;

interface Rect { x: number; y: number; width: number; height: number }
interface WindowRecord { window_id: number; pid: number; title?: string; bounds?: Rect; layer?: number; z_index?: number | null; is_on_screen?: boolean; on_current_space?: boolean }
interface Target { pid: number; windowId: number }
interface Snapshot extends Target { id: string; bounds: Rect; scale: number; refs: Set<string> }

const BLOCKED = ["com.openai.codex", "com.apple.Terminal", "com.googlecode.iterm2", "com.apple.systempreferences", "com.apple.loginwindow", "com.apple.SecurityAgent"];
const MAX_ELEMENTS = 300;
const MAX_ELEMENT_BYTES = 120 * 1024;
const MAX_IMAGE_BYTES = 500 * 1024;
const JPEG_QUALITIES = [70, 45, 25];
const MODIFIERS: Record<string, string> = { meta: "cmd", command: "cmd", cmd: "cmd", control: "ctrl", ctrl: "ctrl", alt: "option", option: "option", shift: "shift" };
const KEYS: Record<string, string> = { enter: "return", backspace: "delete", esc: "escape", arrowleft: "left", arrowright: "right", arrowup: "up", arrowdown: "down", " ": "space" };
const NAMED_KEYS = new Set(["return", "tab", "escape", "space", "delete", "home", "end", "pageup", "pagedown", "left", "right", "up", "down", ...Array.from({ length: 12 }, (_, i) => `f${i + 1}`)]);
const PIXELS_PER_NOTCH = 40;

const fail = (code: string, detail?: string): never => { throw new Error(detail ? `${code}: ${detail}` : code); };
const blocked = (id: string) => BLOCKED.some((entry) => id === entry || id.startsWith(`${entry}.`));
const firstLine = (text: string) => text.split("\n").find((line) => line.trim())?.trim().slice(0, 300) ?? "";

function refusal(output: ToolOutput): string {
  const error = output.structured?.error as { code?: unknown; hint?: unknown } | undefined;
  const code = typeof error?.code === "string" ? error.code : output.errorCode ?? "action_failed";
  const hint = typeof output.structured?.hint === "string" ? output.structured.hint : typeof error?.hint === "string" ? error.hint : firstLine(output.text);
  return hint ? `${code}: ${hint}` : code;
}

export function translateKey(chord: string): { key: string; modifiers: string[] } {
  const parts = chord.split("+").map((part) => part.trim()).filter(Boolean);
  const last = parts.pop();
  if (!last) return fail("invalid_key");
  const modifiers = parts.map((part) => MODIFIERS[part.toLowerCase()] ?? fail("invalid_modifier", part));
  const lower = last.toLowerCase();
  const key = KEYS[lower] ?? lower;
  if (!NAMED_KEYS.has(key) && !/^[a-z0-9]$/.test(key)) fail("unsupported_key", last);
  return { key, modifiers };
}

export class CuaComputer {
  private apps = new Map<string, string>();
  private selected = new Map<string, Target>();
  private snapshots = new Map<string, Snapshot>();
  /** hostPid is the Multica process that owns this broker; its approval dialogs must stay out of reach. */
  constructor(private callTool: CallTool, private hostPid?: number) {}

  async perform(input: Record<string, unknown>): Promise<unknown> {
    const op = typeof input.op === "string" ? input.op : fail("invalid_request");
    if (op === "status") return this.status(false);
    if (op === "request_permissions") return this.status(true);
    if (op === "clear") { this.clear(); return { status: "released" }; }
    if (op === "forget") { this.forget(input.apps); return { status: "released" }; }
    const app = typeof input.app === "string" && input.app ? input.app : fail("invalid_request", "app required");
    if (op === "get_app") return this.resolve(app);
    if (blocked(app)) fail("app_forbidden", "This application cannot be automated");
    if (!this.apps.has(app)) fail("app_not_found", "Get the application first");
    switch (op) {
      case "launch": return this.launch(app);
      case "windows": return { windows: (await this.windows(app)).slice(0, 50).map((w) => ({ id: `${w.pid}:${w.window_id}`, title: (w.title ?? "").slice(0, 160), bounds: w.bounds, onScreen: w.is_on_screen === true, onCurrentSpace: w.on_current_space === true })), truncated: false };
      case "select_window": return this.select(app, input.windowId);
      case "observe": return this.observe(app);
      default: return this.act(app, op, input);
    }
  }

  clear() { this.apps.clear(); this.selected.clear(); this.snapshots.clear(); }

  /** Another turn may claim these apps next; it must not inherit this turn's window or observation. */
  private forget(raw: unknown) {
    if (!Array.isArray(raw)) fail("invalid_request", "apps required");
    for (const app of raw as unknown[]) {
      if (typeof app !== "string") continue;
      this.apps.delete(app); this.selected.delete(app); this.snapshots.delete(app);
    }
  }

  private async run(name: string, args: Record<string, unknown>): Promise<ToolOutput> {
    const output = await this.callTool(name, args);
    if (output.isError || output.structured?.effect === "refused") fail(refusal(output));
    return output;
  }

  private async status(prompt: boolean) {
    const output = await this.run("check_permissions", { prompt });
    return { available: true, platform: "darwin", mode: "background", accessibility: output.structured?.accessibility === true, screenRecording: output.structured?.screen_recording === true };
  }

  private async listApps() {
    const output = await this.run("list_apps", {});
    return (output.structured?.apps ?? []) as { bundle_id?: string; name?: string; running?: boolean }[];
  }

  private async resolve(value: string) {
    const lower = value.toLowerCase();
    const apps = await this.listApps();
    const match = apps.find((entry) => entry.bundle_id === value) ?? apps.find((entry) => entry.bundle_id && entry.name?.toLowerCase() === lower);
    if (!match?.bundle_id) return fail("app_not_found", "Supply an installed app name or bundle ID");
    if (blocked(match.bundle_id)) fail("app_forbidden", "This application cannot be automated");
    const name = match.name || value;
    this.apps.set(match.bundle_id, name);
    return { id: match.bundle_id, name };
  }

  private async launch(app: string) {
    // Reopening a running app can create windows; only cold-start it.
    const running = (await this.listApps()).some((entry) => entry.bundle_id === app && entry.running);
    if (!running) await this.run("launch_app", { bundle_id: app });
    return { id: app };
  }

  private async windows(app: string): Promise<WindowRecord[]> {
    const output = await this.run("list_windows", { app });
    return ((output.structured?.windows ?? []) as WindowRecord[]).filter((w) => (w.layer ?? 0) === 0 && w.pid !== this.hostPid);
  }

  private async select(app: string, raw: unknown) {
    const match = typeof raw === "string" ? /^(\d+):(\d+)$/.exec(raw) : null;
    if (!match) return fail("window_unavailable", "List the app's windows again");
    const target = { pid: Number(match[1]), windowId: Number(match[2]) };
    if (!(await this.windows(app)).some((w) => w.pid === target.pid && w.window_id === target.windowId)) fail("window_unavailable", "List the app's windows again");
    this.selected.set(app, target);
    this.snapshots.delete(app);
    return { status: "selected", requiresObservation: true };
  }

  private async target(app: string): Promise<Target> {
    const windows = await this.windows(app);
    const selected = this.selected.get(app);
    if (selected) {
      if (!windows.some((w) => w.pid === selected.pid && w.window_id === selected.windowId)) fail("window_unavailable", "Select a current window");
      return selected;
    }
    const visible = windows.filter((w) => w.is_on_screen && w.on_current_space !== false);
    const front = visible.reduce<WindowRecord | undefined>((best, w) => !best || (w.z_index ?? -1) > (best.z_index ?? -1) ? w : best, undefined);
    if (!front) return fail("window_unavailable", "The application needs a visible window on the current Space");
    return { pid: front.pid, windowId: front.window_id };
  }

  private async observe(app: string) {
    const target = await this.target(app);
    this.snapshots.delete(app);
    let output: ToolOutput | undefined, image: ToolOutput["images"][number] | undefined;
    for (const quality of JPEG_QUALITIES) {
      output = await this.run("get_window_state", { pid: target.pid, window_id: target.windowId, tree_format: "elements", screenshot_format: "jpeg", screenshot_quality: quality, max_image_dimension: 1400, max_elements: MAX_ELEMENTS, timeout_ms: 3000 });
      image = output.images[0];
      if (image && Math.floor(image.dataBase64.length * 3 / 4) <= MAX_IMAGE_BYTES) break;
      image = undefined;
    }
    const state = output?.structured ?? {};
    if (!image) return fail("image_too_large", "Narrow the target window");
    const bounds = state.window_bounds as Rect | undefined;
    const id = typeof state.snapshot_id === "string" ? state.snapshot_id : undefined;
    const width = Number(state.screenshot_width), height = Number(state.screenshot_height);
    if (!bounds || !id || !(bounds.width > 0) || !(width > 0)) return fail("window_unavailable", "The window could not be captured");
    const refs = new Set<string>();
    const elements: Record<string, unknown>[] = [];
    for (const raw of (state.elements ?? []) as Record<string, unknown>[]) {
      if (elements.length >= MAX_ELEMENTS || typeof raw.element_token !== "string") continue;
      const frame = raw.frame as { x: number; y: number; w: number; h: number } | undefined;
      const value = typeof raw.value === "string" ? raw.value.slice(0, 160) : typeof raw.value === "number" || typeof raw.value === "boolean" ? raw.value : undefined;
      elements.push({
        ref: raw.element_token, role: raw.role, depth: raw.depth,
        ...(typeof raw.label === "string" && raw.label ? { title: raw.label.slice(0, 160) } : {}),
        ...(value !== undefined ? { value } : {}),
        ...(typeof raw.enabled === "boolean" ? { enabled: raw.enabled } : {}),
        ...(raw.selected === true ? { selected: true } : {}),
        ...(frame ? { bounds: { x: frame.x - bounds.x, y: frame.y - bounds.y, width: frame.w, height: frame.h } } : {}),
        ...(Array.isArray(raw.actions) && raw.actions.length ? { actions: raw.actions.slice(0, 6) } : {}),
      });
      refs.add(raw.element_token);
    }
    let truncated = state.truncated === true;
    while (elements.length && JSON.stringify(elements).length > MAX_ELEMENT_BYTES) { refs.delete(elements.pop()!.ref as string); truncated = true; }
    this.snapshots.set(app, { ...target, id, bounds, scale: width / bounds.width, refs });
    const degraded = typeof state.degraded_reason === "string" ? firstLine(state.degraded_reason) : undefined;
    return {
      appId: app, observationId: id, window: bounds, elements, truncated,
      image: { mimeType: image.mimeType, data: image.dataBase64, width, height },
      coordinateSpace: "window-logical-points", screenshotSize: { width, height }, untrustedContent: true,
      ...(degraded ? { degraded } : {}),
    };
  }

  private async act(app: string, op: string, input: Record<string, unknown>) {
    const snap = this.snapshots.get(app);
    this.snapshots.delete(app);
    if (!snap || snap.id !== input.observationId) return fail("stale_observation", "Observe the application again");
    const window = { pid: snap.pid, window_id: snap.windowId };
    const ref = () => typeof input.ref === "string" && snap.refs.has(input.ref) ? input.ref : fail("stale_control", "Use a ref from the latest observation");
    const point = (xKey = "x", yKey = "y") => {
      const x = input[xKey], y = input[yKey];
      if (typeof x !== "number" || typeof y !== "number" || x < 0 || y < 0 || x >= snap.bounds.width || y >= snap.bounds.height) return fail("invalid_coordinate");
      return { x: x * snap.scale, y: y * snap.scale };
    };
    let output: ToolOutput;
    switch (op) {
      case "click":
        output = input.ref !== undefined ? await this.run("click", { element_token: ref() }) : await this.run("click", { ...window, ...point() });
        break;
      case "set_value":
        output = await this.run("set_value", { element_token: ref(), value: input.value });
        break;
      case "type":
        output = await this.run("type_text", { ...window, text: input.text });
        break;
      case "press": {
        const { key, modifiers } = translateKey(String(input.key ?? ""));
        output = modifiers.length ? await this.run("hotkey", { ...window, keys: [...modifiers, key] }) : await this.run("press_key", { ...window, key });
        break;
      }
      case "scroll": {
        const dy = Number(input.dy);
        if (!Number.isInteger(dy) || dy === 0) fail("invalid_scroll");
        const { x, y } = point();
        output = await this.run("scroll", { ...window, x, y, direction: dy > 0 ? "down" : "up", amount: Math.min(50, Math.max(1, Math.round(Math.abs(dy) / PIXELS_PER_NOTCH))) });
        break;
      }
      case "drag": {
        const from = point(), to = point("toX", "toY");
        output = await this.run("drag", { ...window, from_x: from.x, from_y: from.y, to_x: to.x, to_y: to.y });
        break;
      }
      default: return fail("unsupported_action");
    }
    const effect = output.structured?.effect;
    const hint = output.structured?.hint;
    return { status: "applied", requiresObservation: true, ...(typeof effect === "string" ? { effect } : {}), ...(typeof hint === "string" ? { hint: hint.slice(0, 240) } : {}) };
  }
}
