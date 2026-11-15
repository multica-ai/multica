/**
 * A voice-input stream to Deepgram Flux, opened with a short-lived token the
 * server mints. Flux decides when the speaker has finished a sentence
 * (EndOfTurn); until then its running transcript is the sentence in progress.
 *
 * The token is only checked when the socket opens. An open stream runs until
 * it is finished or discarded, however long that takes.
 */
import {
  VoiceRecorderError,
  type VoiceDecodeOptions,
  type VoiceMeter,
  type VoiceStream,
  type VoiceStreamHandlers,
} from "@multica/core/voice";
import type { VoiceSession } from "@multica/core/api";
import { LevelMeter, failureOf, openMicrophone } from "./recorder";

/** Flux's input format; the worklet converts the microphone to it. */
const SAMPLE_RATE = 16000;
/** A stream stops itself here. Flux has no limit, so this only bounds a forgotten mic. */
const MAX_STREAM_MS = 10 * 60_000;
/** How long finishing waits for Flux to end the sentence in progress. */
const CLOSE_WAIT_MS = 2000;
/** A stream that has not opened by now is not going to. */
const OPEN_TIMEOUT_MS = 10_000;

/**
 * Mic audio to 16-bit mono PCM at the target rate, in 80 ms chunks (Flux's
 * recommended size). Resampled here rather than by asking for a 16 kHz
 * AudioContext, because Firefox refuses a MediaStreamSource whose rate
 * differs from the device's.
 */
const PCM_WORKLET = `
class PcmDownsampler extends AudioWorkletProcessor {
  constructor(options) {
    super();
    this.step = sampleRate / options.processorOptions.targetRate;
    this.pos = 0;
    this.buf = new Float32Array(0);
    this.chunk = new Int16Array(Math.round(options.processorOptions.targetRate * 0.08));
    this.fill = 0;
  }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) return true;
    const buf = new Float32Array(this.buf.length + ch.length);
    buf.set(this.buf);
    buf.set(ch, this.buf.length);
    let pos = this.pos;
    while (pos + 1 < buf.length) {
      const i = Math.floor(pos);
      const f = pos - i;
      const s = Math.max(-1, Math.min(1, buf[i] * (1 - f) + buf[i + 1] * f));
      this.chunk[this.fill++] = s < 0 ? s * 0x8000 : s * 0x7fff;
      if (this.fill === this.chunk.length) {
        this.port.postMessage(this.chunk.slice().buffer);
        this.fill = 0;
      }
      pos += this.step;
    }
    const used = Math.floor(pos);
    this.buf = buf.slice(used);
    this.pos = pos - used;
    return true;
  }
}
registerProcessor("pcm-downsampler", PcmDownsampler);
`;

async function loadWorklet(audio: AudioContext) {
  const url = URL.createObjectURL(new Blob([PCM_WORKLET], { type: "text/javascript" }));
  try {
    await audio.audioWorklet.addModule(url);
  } finally {
    URL.revokeObjectURL(url);
  }
}

interface FluxMessage {
  type?: string;
  event?: string;
  transcript?: string;
}

export class DeepgramStream implements VoiceStream {
  private audio: AudioContext | null = null;
  private media: MediaStream | null = null;
  private socket: WebSocket | null = null;
  private worklet: AudioWorkletNode | null = null;
  private meter: LevelMeter | null = null;
  /** The sentence Flux is still hearing, typed as is if it never ends the turn. */
  private pending = "";
  private stopping = false;
  private closed = false;
  /** Rejects a start still waiting for the socket, when the stream is discarded. */
  private abortOpen: ((err: Error) => void) | null = null;

  constructor(
    private readonly handlers: VoiceStreamHandlers,
    private readonly onMeter: (meter: VoiceMeter) => void,
    private readonly onLimit: () => void,
    private readonly openSession: () => Promise<VoiceSession>,
  ) {}

  async start(deviceId: string | null, { hotwords }: VoiceDecodeOptions): Promise<void> {
    // Created and resumed before the first await, inside the press: iOS
    // Safari only lets a user gesture start audio.
    const audio = new AudioContext();
    this.audio = audio;
    if (audio.state === "suspended") void audio.resume().catch(() => {});
    try {
      this.media = await openMicrophone(deviceId);
    } catch (err) {
      this.release();
      throw new VoiceRecorderError(failureOf(err));
    }
    if (this.closed) return this.release();
    this.handlers.onMicrophone?.();
    try {
      await loadWorklet(audio);
      if (this.closed) return this.release();
      // The token comes after the permission prompt, which can outlast it.
      const session = await this.openSession();
      if (this.closed) return this.release();
      if (session.provider !== "deepgram" || !session.url || !session.token) {
        throw new Error(`unsupported voice session: ${session.provider || "none"}`);
      }
      const url = new URL(session.url);
      for (const word of hotwords) url.searchParams.append("keyterm", word);
      const socket = new WebSocket(url, ["bearer", session.token]);
      socket.binaryType = "arraybuffer";
      this.socket = socket;
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error("the speech provider did not answer")), OPEN_TIMEOUT_MS);
        const settle = (err?: Error) => {
          clearTimeout(timer);
          this.abortOpen = null;
          if (err) reject(err);
          else resolve();
        };
        this.abortOpen = settle;
        socket.onopen = () => settle();
        socket.onerror = () => settle(new Error("the speech provider refused the stream"));
        socket.onclose = () => settle(new Error("the speech provider closed the stream"));
      });
      if (this.closed) return this.release();
      socket.onmessage = (event) => this.receive(event.data);
      socket.onerror = null;
      socket.onclose = () => {
        if (!this.stopping && !this.closed) {
          this.release();
          this.handlers.onError();
        }
      };

      const source = audio.createMediaStreamSource(this.media);
      const worklet = new AudioWorkletNode(audio, "pcm-downsampler", {
        processorOptions: { targetRate: SAMPLE_RATE },
      });
      worklet.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
        if (socket.readyState === WebSocket.OPEN) socket.send(event.data);
      };
      source.connect(worklet);
      // A silent path to the speakers keeps every browser pulling audio
      // through the worklet; a node with no route to the destination may not run.
      const silence = audio.createGain();
      silence.gain.value = 0;
      worklet.connect(silence).connect(audio.destination);
      this.worklet = worklet;
      this.meter = new LevelMeter(audio, source, (meter) => {
        this.onMeter(meter);
        if (meter.elapsedMs >= MAX_STREAM_MS) this.onLimit();
      });
      if (audio.state === "suspended") await audio.resume();
      if (this.closed) throw new Error("the stream ended while starting");
    } catch (err) {
      this.release();
      throw err;
    }
  }

  private receive(data: unknown) {
    if (typeof data !== "string" || this.closed) return;
    let message: FluxMessage;
    try {
      message = JSON.parse(data) as FluxMessage;
    } catch {
      return;
    }
    if (message.type !== "TurnInfo") return;
    const text = (message.transcript ?? "").trim();
    if (message.event === "EndOfTurn") {
      this.pending = "";
      this.handlers.onInterim("");
      if (text) this.handlers.onTurn(text);
    } else if (text) {
      this.pending = text;
      this.handlers.onInterim(text);
    }
  }

  /** Stop the microphone, let Flux end the sentence in progress, then close. */
  finish(): Promise<void> {
    const socket = this.socket;
    this.stopping = true;
    this.stopAudio();
    if (!socket || socket.readyState !== WebSocket.OPEN || this.closed) {
      this.release();
      return Promise.resolve();
    }
    return new Promise((resolve) => {
      const done = () => {
        clearTimeout(timer);
        if (!this.closed && this.pending) this.handlers.onTurn(this.pending);
        this.pending = "";
        this.release();
        resolve();
      };
      const timer = setTimeout(done, CLOSE_WAIT_MS);
      socket.onclose = done;
      try {
        socket.send(JSON.stringify({ type: "CloseStream" }));
      } catch {
        done();
      }
    });
  }

  /** Stop without delivering anything more. Safe to call at any point, repeatedly. */
  discard() {
    this.release();
  }

  private stopAudio() {
    // Drop the worklet's queued chunks first: Flux rejects audio after CloseStream.
    if (this.worklet) {
      this.worklet.port.onmessage = null;
      this.worklet.disconnect();
      this.worklet = null;
    }
    this.meter?.stop();
    this.meter = null;
    this.media?.getTracks().forEach((track) => track.stop());
    this.media = null;
  }

  private release() {
    this.closed = true;
    this.abortOpen?.(new Error("the stream was discarded"));
    this.stopAudio();
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
      if (socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.OPEN) socket.close();
    }
    const audio = this.audio;
    this.audio = null;
    if (audio && audio.state !== "closed") void audio.close().catch(() => {});
  }
}
