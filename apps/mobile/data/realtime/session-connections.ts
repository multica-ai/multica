const disconnectors = new Set<() => void>();

export function registerSessionConnection(disconnect: () => void): () => void {
  disconnectors.add(disconnect);
  return () => { disconnectors.delete(disconnect); };
}

/** Synchronous, before a new instance or credential can be published. */
export function stopSessionConnections(): void {
  for (const disconnect of disconnectors) disconnect();
  disconnectors.clear();
}
