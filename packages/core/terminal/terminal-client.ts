import type { TerminalWebSocketConfig } from "../types";

const MAGIC_0 = 0x4d;
const MAGIC_1 = 0x54;
const VERSION = 1;
const KIND_OUTPUT = 1;
const KIND_INPUT = 2;
const HEADER_SIZE = 28;
const MAX_PAYLOAD = 32 * 1024;
const RECONNECT_MAX_MS = 30_000;

export type TerminalConnectionState = "connecting" | "connected" | "reconnecting" | "disconnected";

export interface TerminalControlState {
  controller: boolean;
  leaseToken?: string;
  leaseExpiresAt?: string;
}

export interface TerminalServerMessage {
  type: string;
  protocol_version?: number;
  session_id?: string;
  status?: string;
  structured_observation?: "available" | "stale" | "unavailable";
  cols?: number;
  rows?: number;
  output_seq?: number;
  oldest_seq?: number;
  controller?: boolean;
  lease_token?: string;
  lease_expires_at?: string;
  exit_code?: number;
  error?: string;
}

export interface TerminalClientHandlers {
  onOutput: (data: Uint8Array, sequence: number) => void;
  onMessage?: (message: TerminalServerMessage) => void;
  onConnectionState?: (state: TerminalConnectionState) => void;
  onControl?: (state: TerminalControlState) => void;
  onError?: (message: string) => void;
}

export class TerminalClient {
  private socket: WebSocket | null = null;
  private sessionId: string | null = null;
  private lastSequence = 0;
  private inputSequence = randomInputSequenceBase();
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private renewTimer: ReturnType<typeof setInterval> | null = null;
  private leaseToken: string | null = null;
  private stopped = false;
  private ackTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private readonly config: () => TerminalWebSocketConfig,
    private readonly handlers: TerminalClientHandlers,
  ) {}

  connect(): void {
    this.stopped = false;
    this.open(false);
  }

  disconnect(): void {
    this.stopped = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    if (this.renewTimer) clearInterval(this.renewTimer);
    if (this.ackTimer) clearTimeout(this.ackTimer);
    this.reconnectTimer = null;
    this.renewTimer = null;
    this.ackTimer = null;
    this.socket?.close();
    this.socket = null;
    this.handlers.onConnectionState?.("disconnected");
  }

  claimControl(): void {
    this.sendJSON({ type: "claim_control", session_id: this.sessionId });
  }

  releaseControl(): void {
    if (this.leaseToken) {
      this.sendJSON({
        type: "release_control",
        session_id: this.sessionId,
        lease_token: this.leaseToken,
      });
    }
    this.setControl({ controller: false });
  }

  resize(cols: number, rows: number): void {
    this.sendJSON({ type: "resize", session_id: this.sessionId, cols, rows });
  }

  ctrlC(): void {
    this.sendJSON({ type: "ctrl_c", session_id: this.sessionId });
  }

  sendInput(data: string): void {
    if (!this.sessionId || this.socket?.readyState !== WebSocket.OPEN || !this.leaseToken) return;
    const encoded = new TextEncoder().encode(data);
    for (let offset = 0; offset < encoded.length; offset += MAX_PAYLOAD) {
      const payload = encoded.subarray(offset, Math.min(offset + MAX_PAYLOAD, encoded.length));
      this.inputSequence += 1n;
      this.socket.send(encodeBinary(KIND_INPUT, this.sessionId, this.inputSequence, payload));
    }
  }

  private open(reconnecting: boolean): void {
    const cfg = this.config();
    this.handlers.onConnectionState?.(reconnecting ? "reconnecting" : "connecting");
    const socket = new WebSocket(cfg.url);
    socket.binaryType = "arraybuffer";
    this.socket = socket;
    socket.onopen = () => {
      if (cfg.token) {
        socket.send(JSON.stringify({ type: "auth", payload: { token: cfg.token } }));
      } else {
        this.attach();
      }
    };
    socket.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        this.handleBinary(new Uint8Array(event.data));
        return;
      }
      if (typeof event.data !== "string") return;
      let message: TerminalServerMessage;
      try {
        message = JSON.parse(event.data) as TerminalServerMessage;
      } catch {
        this.handlers.onError?.("Received an invalid terminal control message.");
        return;
      }
      if (message.type === "auth_ack") {
        this.attach();
        return;
      }
      if (message.type === "attached") {
        this.sessionId = message.session_id ?? null;
        this.reconnectAttempt = 0;
        this.handlers.onConnectionState?.("connected");
      } else if (message.type === "control") {
        this.setControl({ controller: message.controller === true, leaseToken: message.lease_token, leaseExpiresAt: message.lease_expires_at });
      } else if (message.type === "error" && message.error) {
        this.handlers.onError?.(message.error);
      }
      this.handlers.onMessage?.(message);
    };
    socket.onclose = () => {
      if (this.socket === socket) this.socket = null;
      this.setControl({ controller: false });
      if (!this.stopped) this.scheduleReconnect();
    };
    socket.onerror = () => {
      // onclose owns reconnect; browsers intentionally expose no useful detail.
    };
  }

  private attach(): void {
    this.sendJSON({ type: "attach", protocol_version: VERSION, last_seq: this.lastSequence });
  }

  private handleBinary(raw: Uint8Array): void {
    const decoded = decodeBinary(raw);
    if (!decoded || decoded.kind !== KIND_OUTPUT) {
      this.handlers.onError?.("Received an invalid terminal output frame.");
      return;
    }
    if (this.sessionId && decoded.sessionId !== this.sessionId) return;
    if (decoded.sequence <= this.lastSequence) return;
    this.lastSequence = decoded.sequence;
    this.handlers.onOutput(decoded.payload, decoded.sequence);
    if (!this.ackTimer) {
      this.ackTimer = setTimeout(() => {
        this.ackTimer = null;
        this.sendJSON({ type: "ack", session_id: this.sessionId, last_seq: this.lastSequence });
      }, 100);
    }
  }

  private scheduleReconnect(): void {
    this.handlers.onConnectionState?.("reconnecting");
    const base = Math.min(1_000 * 2 ** this.reconnectAttempt, RECONNECT_MAX_MS);
    const delay = Math.round(base * (0.8 + Math.random() * 0.4));
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => this.open(true), delay);
  }

  private sendJSON(value: unknown): void {
    if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify(value));
  }

  private setControl(state: TerminalControlState): void {
    if (this.renewTimer) clearInterval(this.renewTimer);
    this.renewTimer = null;
    this.leaseToken = state.controller ? state.leaseToken ?? this.leaseToken : null;
    if (state.controller && this.leaseToken) {
      this.renewTimer = setInterval(() => {
        this.sendJSON({
          type: "renew_control",
          session_id: this.sessionId,
          lease_token: this.leaseToken,
        });
      }, 10_000);
    }
    this.handlers.onControl?.({ ...state, leaseToken: this.leaseToken ?? undefined });
  }
}

function encodeBinary(kind: number, sessionId: string, sequence: bigint, payload: Uint8Array): ArrayBuffer {
  const bytes = new Uint8Array(HEADER_SIZE + payload.length);
  bytes[0] = MAGIC_0;
  bytes[1] = MAGIC_1;
  bytes[2] = VERSION;
  bytes[3] = kind;
  bytes.set(uuidToBytes(sessionId), 4);
  new DataView(bytes.buffer).setBigUint64(20, sequence, false);
  bytes.set(payload, HEADER_SIZE);
  return bytes.buffer;
}

function decodeBinary(raw: Uint8Array): { kind: number; sessionId: string; sequence: number; payload: Uint8Array } | null {
  if (raw.length <= HEADER_SIZE || raw.length - HEADER_SIZE > MAX_PAYLOAD || raw[0] !== MAGIC_0 || raw[1] !== MAGIC_1 || raw[2] !== VERSION) return null;
  const sequence = Number(new DataView(raw.buffer, raw.byteOffset, raw.byteLength).getBigUint64(20, false));
  return { kind: raw[3] ?? 0, sessionId: bytesToUuid(raw.subarray(4, 20)), sequence, payload: raw.slice(HEADER_SIZE) };
}

function uuidToBytes(value: string): Uint8Array {
  const hex = value.replaceAll("-", "");
  if (!/^[0-9a-fA-F]{32}$/.test(hex)) throw new Error("invalid terminal session id");
  const bytes = new Uint8Array(16);
  for (let i = 0; i < bytes.length; i += 1) bytes[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return bytes;
}

function bytesToUuid(bytes: Uint8Array): string {
  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function randomInputSequenceBase(): bigint {
  const words = new Uint32Array(1);
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(words);
  } else {
    words[0] = Math.floor(Math.random() * 0xffffffff);
  }
  return BigInt(words[0] ?? 0) << 32n;
}
