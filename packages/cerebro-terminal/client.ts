// Re-export and tiny helpers for the terminal feature. Everything routes
// through the shared api client (CEREBRO-PATCH(api-client-terminal)) so we
// don't carry a parallel fetch stack here.

export type PresentationMode = "headless" | "interactive";

export interface TerminalSessionInfo {
  id: string;
  runtime_id: string;
  command: string[];
  created_at: string;
  attach_path: string;
}

export type TerminalFrame =
  | { type: "stdout"; data: string /* base64 */ }
  | { type: "exit"; code: number; error?: string }
  // CEREBRO-PATCH(terminal-ws-auth): auth handshake frames sent by server before streaming starts.
  | { type: "auth_ack" }
  | { type: "auth_error"; error?: string };

export type TerminalOutgoing = { type: "stdin"; data: string /* base64 */ };

export function encodeStdin(input: string | Uint8Array): TerminalOutgoing {
  const bytes =
    typeof input === "string" ? new TextEncoder().encode(input) : input;
  let binary = "";
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]!);
  }
  return { type: "stdin", data: btoa(binary) };
}

export function decodeStdout(data: string): Uint8Array {
  const binary = atob(data);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}
