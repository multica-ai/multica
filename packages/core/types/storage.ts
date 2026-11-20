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
}
