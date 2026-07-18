/**
 * Main-process messages that must wait until the main renderer has installed
 * the matching listener. A BrowserWindow finishing `loadURL` is not enough:
 * React effects subscribe later, so an eager deep link can otherwise vanish.
 */
export const MAIN_RENDERER_CHANNEL_STATE_CHANNEL =
  "main-renderer:channel-state";

export const MAIN_RENDERER_MESSAGE_CHANNELS = [
  "auth:token",
  "invite:open",
  "inbox:open",
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

type SendMessage = (
  channel: MainRendererMessageChannel,
  payload: unknown,
) => void;

/**
 * Holds renderer-bound messages until the specific React listener is ready.
 * Readiness is per BrowserWindow lifecycle; pending messages intentionally
 * survive a main-window close so recreating the window still delivers them.
 */
export class MainRendererMessageQueue {
  private readonly readyChannels = new Set<MainRendererMessageChannel>();
  private readonly pending = new Map<
    MainRendererMessageChannel,
    unknown[]
  >();

  enqueue(
    channel: MainRendererMessageChannel,
    payload: unknown,
    send: SendMessage,
  ): void {
    if (this.readyChannels.has(channel)) {
      send(channel, payload);
      return;
    }
    const queued = this.pending.get(channel) ?? [];
    queued.push(payload);
    this.pending.set(channel, queued);
  }

  setReady(
    channel: MainRendererMessageChannel,
    ready: boolean,
    send: SendMessage,
  ): void {
    if (!ready) {
      this.readyChannels.delete(channel);
      return;
    }

    this.readyChannels.add(channel);
    const queued = this.pending.get(channel);
    if (!queued) return;
    this.pending.delete(channel);
    for (const payload of queued) send(channel, payload);
  }

  /** Clear readiness when the main renderer is replaced, without losing work. */
  resetReady(): void {
    this.readyChannels.clear();
  }

  /** Drop messages that are no longer safe for the active account. */
  clear(channel: MainRendererMessageChannel): void {
    this.pending.delete(channel);
  }
}
