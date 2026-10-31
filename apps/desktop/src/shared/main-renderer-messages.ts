/**
 * Main-process messages that must wait until a main renderer has installed
 * the matching listener. A BrowserWindow finishing `loadURL` is not enough:
 * React effects subscribe later, so an eager deep link can otherwise vanish.
 */
export const MAIN_RENDERER_CHANNEL_STATE_CHANNEL =
  "main-renderer:channel-state";

export const TAB_SELECTION_SHORTCUT_CHANNEL = "tab:select-by-shortcut";

export const TAB_SELECTION_SHORTCUT_KEYS = [1, 2, 3, 4, 5, 6, 7, 8, 9] as const;

export type TabSelectionShortcutKey =
  (typeof TAB_SELECTION_SHORTCUT_KEYS)[number];

export function parseTabSelectionShortcutKey(
  value: unknown,
): TabSelectionShortcutKey | null {
  return typeof value === "number" &&
    TAB_SELECTION_SHORTCUT_KEYS.includes(value as TabSelectionShortcutKey)
    ? (value as TabSelectionShortcutKey)
    : null;
}

/** Open a workspace-scoped path in the window that shows that workspace. */
export const WORKSPACE_OPEN_PATH_CHANNEL = "workspace:open-path";
/** Cmd/Ctrl+Shift+N: the renderer picks which workspace gets the new window. */
export const NEW_WINDOW_SHORTCUT_CHANNEL = "window:new-by-shortcut";

export const MAIN_RENDERER_MESSAGE_CHANNELS = [
  "auth:token",
  "invite:open",
  "inbox:open",
  "settings:open",
  TAB_SELECTION_SHORTCUT_CHANNEL,
  WORKSPACE_OPEN_PATH_CHANNEL,
  NEW_WINDOW_SHORTCUT_CHANNEL,
] as const;

export type MainRendererMessageChannel =
  (typeof MAIN_RENDERER_MESSAGE_CHANNELS)[number];

export interface MainRendererChannelState {
  channel: MainRendererMessageChannel;
  ready: boolean;
}

const mainRendererMessageChannels = new Set<string>(
  MAIN_RENDERER_MESSAGE_CHANNELS,
);

export function parseMainRendererChannelState(
  value: unknown,
): MainRendererChannelState | null {
  if (!value || typeof value !== "object") return null;
  const candidate = value as Record<string, unknown>;
  if (
    typeof candidate.channel !== "string" ||
    !mainRendererMessageChannels.has(candidate.channel) ||
    typeof candidate.ready !== "boolean"
  ) {
    return null;
  }
  return {
    channel: candidate.channel as MainRendererMessageChannel,
    ready: candidate.ready,
  };
}

type SendMessage<W> = (
  target: W,
  channel: MainRendererMessageChannel,
  payload: unknown,
) => void;

/**
 * Holds renderer-bound messages until the specific React listener in the
 * target main window is ready. Readiness is per window lifecycle; a message
 * with no target (sent before any window exists), or whose target closed
 * before it was delivered, goes to the next main window that becomes ready,
 * so recreating a window still delivers it.
 */
export class MainRendererMessageQueue<W> {
  private readonly readyChannels = new Map<W, Set<MainRendererMessageChannel>>();
  private readonly pending = new Map<
    MainRendererMessageChannel,
    { target: W | null; payload: unknown }[]
  >();

  constructor(
    private readonly send: SendMessage<W>,
    private readonly isLive: (window: W) => boolean,
  ) {}

  enqueue(
    target: W | null,
    channel: MainRendererMessageChannel,
    payload: unknown,
  ): void {
    if (target !== null && this.readyChannels.get(target)?.has(channel)) {
      this.send(target, channel, payload);
      return;
    }
    const queued = this.pending.get(channel) ?? [];
    queued.push({ target, payload });
    this.pending.set(channel, queued);
  }

  setReady(window: W, channel: MainRendererMessageChannel, ready: boolean): void {
    if (!ready) {
      this.readyChannels.get(window)?.delete(channel);
      return;
    }

    const channels = this.readyChannels.get(window) ?? new Set();
    channels.add(channel);
    this.readyChannels.set(window, channels);
    const queued = this.pending.get(channel);
    if (!queued) return;
    const deliver = queued.filter(
      (entry) =>
        entry.target === window ||
        entry.target === null ||
        !this.isLive(entry.target),
    );
    const keep = queued.filter((entry) => !deliver.includes(entry));
    if (keep.length > 0) this.pending.set(channel, keep);
    else this.pending.delete(channel);
    for (const entry of deliver) this.send(window, channel, entry.payload);
  }

  /**
   * Forget a window's readiness when its renderer goes away and hand its
   * undelivered messages to `successor`, when another main window exists.
   */
  release(window: W, successor: W | null): void {
    this.readyChannels.delete(window);
    if (!successor) return;
    for (const [channel, queued] of [...this.pending]) {
      const moved = queued.filter((entry) => entry.target === window);
      if (moved.length === 0) continue;
      const keep = queued.filter((entry) => entry.target !== window);
      if (keep.length > 0) this.pending.set(channel, keep);
      else this.pending.delete(channel);
      for (const entry of moved) this.enqueue(successor, channel, entry.payload);
    }
  }

  /** Drop messages that are no longer safe for the active account. */
  clear(channel: MainRendererMessageChannel): void {
    this.pending.delete(channel);
  }
}
