/** Announced by a daemon in /hello when it accepts conversation.focus. */
export const LOCAL_V2_FOCUS_CAPABILITY = "conversation-focus-v1";

/** How long someone must stay on a conversation before it counts as focused. */
export const LOCAL_V2_FOCUS_STABLE_MS = 1500;

/**
 * Tells the daemon which conversations this client is looking at. Passing through
 * a conversation says nothing; leaving one it was told about does. Nothing here
 * depends on who holds a conversation, because when one view takes over from
 * another, its hold can start before or after the other's ends.
 *
 * deliver reports whether the message left this client: what could not be sent,
 * and what the daemon may have lost with its connection, goes out on reconnected.
 */
export class LocalV2FocusTracker {
  private holds = new Map<string, number>();
  private timers = new Map<string, ReturnType<typeof setTimeout>>();
  private told = new Set<string>();
  private unsentBlurs = new Set<string>();

  constructor(private deliver: (id: string, focused: boolean) => boolean | Promise<boolean>, private stableMs = LOCAL_V2_FOCUS_STABLE_MS) {}

  /** Someone is looking at a conversation until the returned function is called. */
  hold(id: string): () => void {
    const held = this.holds.get(id) ?? 0;
    this.holds.set(id, held + 1);
    if (held === 0 && !this.told.has(id)) {
      this.timers.set(id, setTimeout(() => {
        this.timers.delete(id);
        this.told.add(id);
        this.unsentBlurs.delete(id);
        void this.deliver(id, true);
      }, this.stableMs));
    }
    let released = false;
    return () => {
      if (released) return;
      released = true;
      const left = (this.holds.get(id) ?? 1) - 1;
      if (left > 0) { this.holds.set(id, left); return; }
      this.holds.delete(id);
      const timer = this.timers.get(id);
      if (timer !== undefined) { clearTimeout(timer); this.timers.delete(id); }
      if (this.told.delete(id)) void this.blur(id);
    };
  }

  private async blur(id: string): Promise<void> {
    if (!await this.deliver(id, false)) this.unsentBlurs.add(id);
  }

  /** A new connection: the daemon may not know what this client looks at now. */
  reconnected(): void {
    for (const id of [...this.unsentBlurs]) { this.unsentBlurs.delete(id); void this.blur(id); }
    for (const id of this.told) void this.deliver(id, true);
  }

  dispose(): void {
    for (const timer of this.timers.values()) clearTimeout(timer);
    this.timers.clear();
  }
}
