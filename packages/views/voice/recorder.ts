/**
 * One microphone recording: open the device, record, meter the level, and
 * hand back the clip. Knows nothing about composers or transcription.
 */
import {
  VoiceRecorderError,
  type VoiceClip,
  type VoiceMeter,
  type VoiceRecorder,
  type VoiceRecorderFailure,
} from "@multica/core/voice";

/** The recording stops itself here; Desktop's engine accepts a little more. */
const MAX_RECORDING_MS = 120_000;
/** Shorter clips, or clips that never rise above the noise floor, are not sent. */
const MIN_RECORDING_MS = 400;
const SPEECH_LEVEL = 0.04;
/** Enough history to fill a full-width strip at the waveform's 5 px pitch. */
const LEVEL_HISTORY = 288;
export const METER_INTERVAL_MS = 50;

const MIME_TYPES = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"];

export function voiceInputSupported(): boolean {
  return (
    typeof navigator !== "undefined" &&
    typeof navigator.mediaDevices?.getUserMedia === "function" &&
    typeof MediaRecorder !== "undefined"
  );
}

export function failureOf(err: unknown): VoiceRecorderFailure {
  const name = err instanceof DOMException || err instanceof Error ? err.name : "";
  if (name === "NotAllowedError" || name === "SecurityError" || name === "PermissionDeniedError") {
    return "permission-denied";
  }
  if (name === "NotFoundError" || name === "DevicesNotFoundError") return "no-microphone";
  return "failed";
}

export async function openMicrophone(deviceId: string | null): Promise<MediaStream> {
  // Noise suppression and gain control are tuned for calls; they smear
  // consonants and pump the level, which costs recognition accuracy.
  const base = { echoCancellation: true, noiseSuppression: false, autoGainControl: false };
  if (deviceId) {
    try {
      return await navigator.mediaDevices.getUserMedia({ audio: { ...base, deviceId: { exact: deviceId } } });
    } catch (err) {
      // The saved device was unplugged: fall back to the system default.
      const name = err instanceof Error ? err.name : "";
      if (name !== "OverconstrainedError" && name !== "NotFoundError") throw err;
    }
  }
  return navigator.mediaDevices.getUserMedia({ audio: base });
}

/**
 * The level meter both recorders share: samples the input ~20 times a second
 * and reports the level history and the time since it started.
 */
export class LevelMeter {
  private timer: number | null = null;
  private levels: number[] = [];
  private readonly startedAt = performance.now();
  /** The loudest level so far, to tell speech from silence. */
  peak = 0;

  constructor(audio: AudioContext, source: AudioNode, onTick: (meter: VoiceMeter) => void) {
    const analyser = audio.createAnalyser();
    analyser.fftSize = 512;
    source.connect(analyser);
    const samples = new Uint8Array(analyser.fftSize);
    this.timer = window.setInterval(() => {
      analyser.getByteTimeDomainData(samples);
      let sum = 0;
      for (const sample of samples) {
        const v = (sample - 128) / 128;
        sum += v * v;
      }
      const level = Math.min(1, Math.sqrt(sum / samples.length) * 5);
      this.peak = Math.max(this.peak, level);
      this.levels = [...this.levels.slice(-(LEVEL_HISTORY - 1)), level];
      onTick({ levels: this.levels, elapsedMs: performance.now() - this.startedAt });
    }, METER_INTERVAL_MS);
  }

  stop() {
    if (this.timer !== null) window.clearInterval(this.timer);
    this.timer = null;
  }
}

export class Recording implements VoiceRecorder {
  private stream: MediaStream | null = null;
  private recorder: MediaRecorder | null = null;
  private audio: AudioContext | null = null;
  private chunks: Blob[] = [];
  private startedAt = 0;
  private peak = 0;
  private meter: LevelMeter | null = null;
  private closed = false;

  constructor(
    private readonly onMeter: (meter: VoiceMeter) => void,
    private readonly onLimit: () => void,
  ) {}

  /** Resolves once audio is flowing; rejects with a VoiceRecorderError. */
  async start(deviceId: string | null): Promise<void> {
    let stream: MediaStream;
    try {
      stream = await openMicrophone(deviceId);
    } catch (err) {
      throw new VoiceRecorderError(failureOf(err));
    }
    if (this.closed) {
      stream.getTracks().forEach((track) => track.stop());
      return;
    }
    this.stream = stream;
    const mimeType = MIME_TYPES.find((type) => MediaRecorder.isTypeSupported(type));
    try {
      this.recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    } catch {
      this.release();
      throw new VoiceRecorderError("failed");
    }
    this.recorder.ondataavailable = (event) => {
      if (event.data.size > 0) this.chunks.push(event.data);
    };
    this.recorder.start(250);
    this.startedAt = performance.now();
    this.startMeter(stream);
  }

  private startMeter(stream: MediaStream) {
    try {
      this.audio = new AudioContext();
      this.meter = new LevelMeter(this.audio, this.audio.createMediaStreamSource(stream), (meter) => {
        this.peak = this.meter?.peak ?? this.peak;
        this.onMeter(meter);
        if (meter.elapsedMs >= MAX_RECORDING_MS) this.onLimit();
      });
    } catch {
      // Metering is cosmetic; record without it. Without a meter there is no
      // level to judge silence by, so treat the clip as speech.
      this.peak = 1;
    }
  }

  /** Stop and return the clip. */
  finish(): Promise<VoiceClip> {
    const recorder = this.recorder;
    const durationMs = performance.now() - this.startedAt;
    const heardSpeech = durationMs >= MIN_RECORDING_MS && this.peak >= SPEECH_LEVEL;
    return new Promise((resolve) => {
      const done = () => {
        const type = recorder?.mimeType || this.chunks[0]?.type || "audio/webm";
        const blob = new Blob(this.chunks, { type });
        this.release();
        resolve({ blob, durationMs, heardSpeech: heardSpeech && blob.size > 0 });
      };
      if (!recorder || recorder.state === "inactive") {
        done();
        return;
      }
      recorder.onstop = done;
      recorder.stop();
    });
  }

  /** Stop without keeping anything. Safe to call at any point, repeatedly. */
  discard() {
    this.closed = true;
    if (this.recorder && this.recorder.state !== "inactive") {
      this.recorder.onstop = null;
      this.recorder.stop();
    }
    this.chunks = [];
    this.release();
  }

  private release() {
    this.closed = true;
    this.meter?.stop();
    this.meter = null;
    this.stream?.getTracks().forEach((track) => track.stop());
    this.stream = null;
    void this.audio?.close().catch(() => {});
    this.audio = null;
  }
}
