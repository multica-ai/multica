export interface StorageAdapter {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  /**
   * Every key currently held, for callers that must clean up entries they
   * cannot name — workspace-scoped keys are `${base}:${slug}`, and a process
   * that never resolved a workspace list has no way to reconstruct the slugs.
   *
   * Optional because it is a real capability boundary, not a compatibility
   * shim: a backend with asynchronous or opaque storage genuinely cannot
   * answer this synchronously. Callers state what they degrade to without it.
   */
  keys?(): string[];
  /**
   * Call `listener` when another browsing context (another tab or window of
   * the same origin) changes a key; `null` means it cleared everything. Writes
   * made through this adapter in the current context are not reported.
   *
   * Optional for the same reason as `keys`: not every backend can observe
   * other writers. Callers without it only see their own writes.
   */
  subscribe?(listener: (key: string | null) => void): () => void;
  /**
   * Read-modify-write `key` as one step that no other tab or window of the
   * same origin can interleave with. `updater` gets the current value and
   * returns the value to store, or `null` to leave the key as it is. Rejects
   * if the write fails (for example, the quota is exceeded).
   *
   * Optional: callers without it fall back to `getItem` + `setItem`, which is
   * only atomic within the current context.
   */
  update?(key: string, updater: (current: string | null) => string | null): Promise<void>;
}
