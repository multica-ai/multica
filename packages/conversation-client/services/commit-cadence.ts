/** About two frames: streaming text stays smooth without a render per message. */
export const COMMIT_INTERVAL_MS = 30;

/**
 * Holds changed conversations until the next commit, so a burst of event messages
 * renders once. It runs on a timer rather than requestAnimationFrame, which pauses
 * in a hidden or occluded window and fires up to 120 times a second otherwise.
 */
export class CommitCadence<Key> {
  private readonly pending = new Set<Key>();
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly commit: (key: Key) => void, private readonly intervalMs = COMMIT_INTERVAL_MS) {}

  schedule(key: Key): void {
    this.pending.add(key);
    this.timer ??= setTimeout(() => {
      this.timer = null;
      this.flush();
    }, this.intervalMs);
  }

  /** Commits now: whatever the user does next must act on the latest state. */
  flush(): void {
    this.cancelTimer();
    if (!this.pending.size) return;
    const keys = [...this.pending];
    this.pending.clear();
    for (const key of keys) this.commit(key);
  }

  dispose(): void {
    this.cancelTimer();
    this.pending.clear();
  }

  private cancelTimer(): void {
    if (this.timer === null) return;
    clearTimeout(this.timer);
    this.timer = null;
  }
}
