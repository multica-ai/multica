import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TerminalClient } from "./terminal-client";

const sessionId = "019fe469-33bc-75c2-9492-ca640a1788a4";

class FakeWebSocket {
  static readonly OPEN = 1;
  static instances: FakeWebSocket[] = [];

  binaryType = "";
  readyState = FakeWebSocket.OPEN;
  sent: Array<string | ArrayBuffer> = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string | ArrayBuffer }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  send(value: string | ArrayBuffer) {
    this.sent.push(value);
  }

  close() {
    this.readyState = 3;
  }

  open() {
    this.onopen?.();
  }

  receive(data: string | ArrayBuffer) {
    this.onmessage?.({ data });
  }

  serverClose() {
    this.readyState = 3;
    this.onclose?.();
  }
}

function outputFrame(sequence: bigint, payload: string): ArrayBuffer {
  const body = new TextEncoder().encode(payload);
  const bytes = new Uint8Array(28 + body.length);
  bytes.set([0x4d, 0x54, 1, 1]);
  const hex = sessionId.replaceAll("-", "");
  for (let index = 0; index < 16; index += 1) {
    bytes[4 + index] = Number.parseInt(hex.slice(index * 2, index * 2 + 2), 16);
  }
  new DataView(bytes.buffer).setBigUint64(20, sequence, false);
  bytes.set(body, 28);
  return bytes.buffer;
}

function jsonMessages(socket: FakeWebSocket) {
  return socket.sent
    .filter((value): value is string => typeof value === "string")
    .map((value) => JSON.parse(value) as Record<string, unknown>);
}

beforeEach(() => {
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("TerminalClient", () => {
  it("authenticates, attaches, relays output, and gates input behind a lease", () => {
    const onOutput = vi.fn();
    const onControl = vi.fn();
    const client = new TerminalClient(
      () => ({ url: "wss://example.test/terminal", token: "secret" }),
      { onOutput, onControl },
    );

    client.connect();
    const socket = FakeWebSocket.instances[0]!;
    socket.open();
    expect(jsonMessages(socket)[0]).toEqual({ type: "auth", payload: { token: "secret" } });
    socket.receive(JSON.stringify({ type: "auth_ack" }));
    expect(jsonMessages(socket)[1]).toEqual({ type: "attach", protocol_version: 1, last_seq: 0 });
    socket.receive(JSON.stringify({ type: "attached", session_id: sessionId }));

    client.sendInput("blocked");
    expect(socket.sent.filter((value) => value instanceof ArrayBuffer)).toHaveLength(0);

    client.claimControl();
    expect(jsonMessages(socket).at(-1)).toMatchObject({ type: "claim_control", session_id: sessionId });
    socket.receive(
      JSON.stringify({
        type: "control",
        controller: true,
        lease_token: "lease",
        lease_expires_at: "2026-08-09T12:00:30Z",
      }),
    );
    client.sendInput("中文");
    const input = socket.sent.find((value) => value instanceof ArrayBuffer) as ArrayBuffer;
    const inputBytes = new Uint8Array(input);
    expect(inputBytes[3]).toBe(2);
    expect(new TextDecoder().decode(inputBytes.slice(28))).toBe("中文");

    socket.receive(outputFrame(1n, "hello"));
    expect(onOutput).toHaveBeenCalledWith(expect.any(Uint8Array), 1);
    expect(new TextDecoder().decode(onOutput.mock.calls[0]?.[0])).toBe("hello");
    expect(onControl).toHaveBeenLastCalledWith(
      expect.objectContaining({ controller: true, leaseToken: "lease" }),
    );
    client.disconnect();
  });

  it("sends resize and Ctrl+C controls and reconnects with the last output sequence", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const states: string[] = [];
    const client = new TerminalClient(
      () => ({ url: "ws://example.test/terminal", token: null }),
      { onOutput: vi.fn(), onConnectionState: (state) => states.push(state) },
    );
    client.connect();
    const first = FakeWebSocket.instances[0]!;
    first.open();
    first.receive(JSON.stringify({ type: "attached", session_id: sessionId }));
    first.receive(
      JSON.stringify({ type: "control", controller: true, lease_token: "lease" }),
    );
    client.resize(140, 45);
    client.ctrlC();
    first.receive(outputFrame(7n, "tail"));
    expect(jsonMessages(first)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ type: "resize", cols: 140, rows: 45 }),
        expect.objectContaining({ type: "ctrl_c" }),
      ]),
    );

    first.serverClose();
    expect(states).toContain("reconnecting");
    vi.advanceTimersByTime(800);
    const second = FakeWebSocket.instances[1]!;
    second.open();
    expect(jsonMessages(second)[0]).toMatchObject({ type: "attach", last_seq: 7 });
    client.disconnect();
  });
});
