import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { VoiceRecorderError, type VoiceStreamHandlers } from "@multica/core/voice";
import { DeepgramStream } from "./deepgram-stream";

class FakeSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  static last: FakeSocket | null = null;
  readyState = FakeSocket.CONNECTING;
  binaryType = "blob";
  sent: unknown[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  constructor(
    readonly url: URL,
    readonly protocols: string[],
  ) {
    FakeSocket.last = this;
  }
  send(data: unknown) {
    this.sent.push(data);
  }
  close() {
    this.readyState = FakeSocket.CLOSED;
  }
  open() {
    this.readyState = FakeSocket.OPEN;
    this.onopen?.();
  }
  message(payload: object) {
    this.onmessage?.({ data: JSON.stringify(payload) });
  }
  closedByServer() {
    this.readyState = FakeSocket.CLOSED;
    this.onclose?.();
  }
}

const node = () => ({ connect: vi.fn(), disconnect: vi.fn() });

class FakeAudioContext {
  state = "running";
  audioWorklet = { addModule: vi.fn(async () => {}) };
  createMediaStreamSource = vi.fn(() => node());
  createAnalyser = vi.fn(() => ({ ...node(), fftSize: 0, getByteTimeDomainData: vi.fn() }));
  close = vi.fn(async () => {
    this.state = "closed";
  });
  resume = vi.fn(async () => {});
}

class FakeWorkletNode {
  port = { onmessage: null as ((event: { data: ArrayBuffer }) => void) | null };
  disconnect = vi.fn();
}

const track = { stop: vi.fn() };
const getUserMedia = vi.fn();
const session = { provider: "deepgram", url: "wss://stream.test/v2/listen?model=flux-general-en", token: "grant", expires_in: 30 };

function handlers() {
  return {
    onInterim: vi.fn(),
    onTurn: vi.fn(),
    onError: vi.fn(),
  } satisfies VoiceStreamHandlers;
}

async function opened(h = handlers(), openSession = vi.fn(async () => session)) {
  const stream = new DeepgramStream(h, vi.fn(), vi.fn(), openSession);
  const started = stream.start(null, { language: "en", hotwords: ["Alice", "Multica"] });
  await vi.waitFor(() => expect(FakeSocket.last).not.toBeNull());
  FakeSocket.last!.open();
  await started;
  return { stream, socket: FakeSocket.last!, h, openSession };
}

beforeEach(() => {
  FakeSocket.last = null;
  track.stop.mockReset();
  getUserMedia.mockReset().mockResolvedValue({ getTracks: () => [track] });
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.stubGlobal("AudioContext", FakeAudioContext);
  vi.stubGlobal("AudioWorkletNode", FakeWorkletNode);
  vi.stubGlobal("navigator", { mediaDevices: { getUserMedia } });
  URL.createObjectURL = vi.fn(() => "blob:worklet");
  URL.revokeObjectURL = vi.fn();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("DeepgramStream", () => {
  it("opens the server's stream with the token as the bearer subprotocol and the hotwords as keyterms", async () => {
    const { socket, openSession } = await opened();
    expect(openSession).toHaveBeenCalledTimes(1);
    expect(socket.protocols).toEqual(["bearer", "grant"]);
    expect(socket.url.origin).toBe("wss://stream.test");
    expect(socket.url.searchParams.get("model")).toBe("flux-general-en");
    expect(socket.url.searchParams.getAll("keyterm")).toEqual(["Alice", "Multica"]);
  });

  it("asks for the token only after the microphone is granted", async () => {
    let grant!: (value: unknown) => void;
    getUserMedia.mockReturnValue(new Promise((resolve) => (grant = resolve)));
    const openSession = vi.fn(async () => session);
    const stream = new DeepgramStream(handlers(), vi.fn(), vi.fn(), openSession);
    const started = stream.start(null, { language: "en", hotwords: [] });
    await Promise.resolve();
    expect(openSession).not.toHaveBeenCalled();
    grant({ getTracks: () => [track] });
    await vi.waitFor(() => expect(FakeSocket.last).not.toBeNull());
    FakeSocket.last!.open();
    await started;
    expect(openSession).toHaveBeenCalledTimes(1);
  });

  it("reports updates as the sentence in progress and EndOfTurn as a finished sentence", async () => {
    const { socket, h } = await opened();
    socket.message({ type: "TurnInfo", event: "StartOfTurn", transcript: "" });
    socket.message({ type: "TurnInfo", event: "Update", transcript: "Assign the" });
    socket.message({ type: "TurnInfo", event: "EagerEndOfTurn", transcript: "Assign the issue" });
    expect(h.onInterim).toHaveBeenLastCalledWith("Assign the issue");
    expect(h.onTurn).not.toHaveBeenCalled();
    socket.message({ type: "TurnInfo", event: "EndOfTurn", transcript: " Assign the issue to Alice. " });
    expect(h.onTurn).toHaveBeenCalledWith("Assign the issue to Alice.");
    expect(h.onInterim).toHaveBeenLastCalledWith("");
  });

  it("finishing asks Flux to close and types the last sentence it ends", async () => {
    const { stream, socket, h } = await opened();
    socket.message({ type: "TurnInfo", event: "Update", transcript: "Ship" });
    const finished = stream.finish();
    expect(socket.sent).toContain(JSON.stringify({ type: "CloseStream" }));
    expect(track.stop).toHaveBeenCalled();
    socket.message({ type: "TurnInfo", event: "EndOfTurn", transcript: "Ship it." });
    socket.closedByServer();
    await finished;
    expect(h.onTurn).toHaveBeenCalledTimes(1);
    expect(h.onTurn).toHaveBeenCalledWith("Ship it.");
    expect(h.onError).not.toHaveBeenCalled();
  });

  it("types the sentence in progress if Flux never ends it", async () => {
    const { stream, socket, h } = await opened();
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    socket.message({ type: "TurnInfo", event: "Update", transcript: "Ship it" });
    const finished = stream.finish();
    vi.advanceTimersByTime(2000);
    await finished;
    expect(h.onTurn).toHaveBeenCalledWith("Ship it");
  });

  it("discarding delivers nothing more", async () => {
    const { stream, socket, h } = await opened();
    stream.discard();
    socket.message({ type: "TurnInfo", event: "EndOfTurn", transcript: "Too late." });
    expect(h.onTurn).not.toHaveBeenCalled();
    expect(socket.readyState).toBe(FakeSocket.CLOSED);
  });

  it("meters the microphone while open and stops metering once discarded", async () => {
    const onMeter = vi.fn();
    const stream = new DeepgramStream(handlers(), onMeter, vi.fn(), vi.fn(async () => session));
    const started = stream.start(null, { language: "en", hotwords: [] });
    await vi.waitFor(() => expect(FakeSocket.last).not.toBeNull());
    FakeSocket.last!.open();
    await started;
    await vi.waitFor(() => expect(onMeter).toHaveBeenCalled());
    stream.discard();
    const calls = onMeter.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(onMeter.mock.calls.length).toBe(calls);
  });

  it("a stream the provider drops is an error", async () => {
    const { socket, h } = await opened();
    socket.closedByServer();
    expect(h.onError).toHaveBeenCalledTimes(1);
    expect(track.stop).toHaveBeenCalled();
  });

  it("a denied microphone is a recorder error and asks for no token", async () => {
    getUserMedia.mockRejectedValue(new DOMException("denied", "NotAllowedError"));
    const openSession = vi.fn(async () => session);
    const stream = new DeepgramStream(handlers(), vi.fn(), vi.fn(), openSession);
    await expect(stream.start(null, { language: "en", hotwords: [] })).rejects.toEqual(
      new VoiceRecorderError("permission-denied"),
    );
    expect(openSession).not.toHaveBeenCalled();
  });

  it("a refused stream rejects and releases the microphone", async () => {
    const stream = new DeepgramStream(handlers(), vi.fn(), vi.fn(), vi.fn(async () => session));
    const started = stream.start(null, { language: "en", hotwords: [] });
    await vi.waitFor(() => expect(FakeSocket.last).not.toBeNull());
    FakeSocket.last!.onerror?.();
    await expect(started).rejects.toThrow();
    expect(track.stop).toHaveBeenCalled();
  });

  it("refuses a session for a provider it does not speak", async () => {
    const stream = new DeepgramStream(handlers(), vi.fn(), vi.fn(), vi.fn(async () => ({ ...session, provider: "other" })));
    await expect(stream.start(null, { language: "en", hotwords: [] })).rejects.toThrow(/unsupported/);
    expect(FakeSocket.last).toBeNull();
  });
});
