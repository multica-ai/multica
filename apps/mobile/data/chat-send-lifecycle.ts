// Client-owned request lifetime, independent of server/optimistic cache data.
// An accepted queued response can retain a placeholder until a snapshot lands.
const activeSends = new Map<string, number>();

export function hasChatSendInFlight(sessionId: string | null) {
  return !!sessionId && (activeSends.get(sessionId) ?? 0) > 0;
}

export function beginChatSend(sessionId: string): () => void {
  activeSends.set(sessionId, (activeSends.get(sessionId) ?? 0) + 1);
  let finished = false;
  return () => {
    if (finished) return;
    finished = true;
    const remaining = (activeSends.get(sessionId) ?? 1) - 1;
    if (remaining > 0) activeSends.set(sessionId, remaining);
    else activeSends.delete(sessionId);
  };
}
