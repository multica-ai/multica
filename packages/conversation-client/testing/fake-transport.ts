import type { LocalV2ClientMessage, LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";

export interface FakeTransport extends LocalV2Transport {
  /** Client messages sent, in order. */
  sent: LocalV2ClientMessage[];
  /** Delivers a server message of the scope, as the socket would. */
  deliver(message: unknown): void;
  connection(connected: boolean): void;
}

/**
 * A daemon for tests, at the transport: everything above it runs for real.
 * A subscribe is confirmed at once, at the cursor it asked for.
 */
export function createFakeTransport(
  scope: LocalV2Scope,
  respond: (method: string, path: string, body: unknown) => unknown | Promise<unknown>,
): FakeTransport {
  const listeners = new Set<(scope: LocalV2Scope, message: unknown) => void>();
  const connections = new Set<(scope: LocalV2Scope, connected: boolean) => void>();
  const sent: LocalV2ClientMessage[] = [];
  const deliver = (message: unknown) => { for (const listener of [...listeners]) listener(scope, message); };
  return {
    sent,
    deliver,
    connection(connected) { for (const listener of [...connections]) listener(scope, connected); },
    request: async (_scope, method, path, body) => respond(method, path, body),
    async send(_scope, message) {
      sent.push(message);
      if (message.type === "subscribe") deliver({ version: message.version, type: "subscription.status", conversation_id: message.conversation_id, status: "subscribed", high_water: message.after_seq });
    },
    subscribe(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    onConnection(listener) { connections.add(listener); return () => { connections.delete(listener); }; },
    fileUrl: (_scope, path) => `fake:${path}`,
    upload: async () => { throw new Error("The fake transport does not upload"); },
    choose: async () => null,
  };
}
