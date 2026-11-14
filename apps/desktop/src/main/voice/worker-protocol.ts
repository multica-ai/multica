import { join } from "path";
import type { RecognizerKind } from "./catalog";

/** Which recognizer to have loaded; `key` changes whenever any field does. */
export interface LoadSpec {
  key: string;
  recognizer: RecognizerKind;
  dir: string;
  /** A VOICE_LANGUAGES value. */
  language: string;
  /** Comma-separated words to favour, for recognizers that take them. */
  hotwords: string;
}

export type WorkerRequest =
  | { id: number; type: "load"; spec: LoadSpec }
  | { id: number; type: "transcribe"; spec: LoadSpec; samples: Float32Array; sampleRate: number };

export type WorkerResponse = { id: number; ok: true; text?: string } | { id: number; ok: false; error: string };

/** sherpa-onnx's offline recognizer config for a model folder. */
export function recognizerConfig(spec: LoadSpec, numThreads: number): Record<string, unknown> {
  const file = (name: string) => join(spec.dir, name);
  let model: Record<string, unknown>;
  if (spec.recognizer === "qwen3-asr") {
    model = {
      qwen3Asr: {
        convFrontend: file("conv_frontend.onnx"),
        encoder: file("encoder.int8.onnx"),
        decoder: file("decoder.int8.onnx"),
        tokenizer: file("tokenizer"),
        hotwords: spec.hotwords,
        // The defaults (512 / 128) cut a segment's transcript short once
        // hotwords lengthen the prompt.
        maxTotalLen: 1024,
        maxNewTokens: 256,
      },
      tokens: "",
    };
  } else if (spec.recognizer === "sense-voice") {
    model = {
      senseVoice: {
        model: file("model.int8.onnx"),
        language: spec.language === "auto" ? "" : spec.language,
        useInverseTextNormalization: 1,
      },
      tokens: file("tokens.txt"),
    };
  } else {
    model = {
      transducer: {
        encoder: file("encoder.int8.onnx"),
        decoder: file("decoder.int8.onnx"),
        joiner: file("joiner.int8.onnx"),
      },
      modelType: "nemo_transducer",
      tokens: file("tokens.txt"),
    };
  }
  return {
    featConfig: { sampleRate: 16000, featureDim: 80 },
    modelConfig: { ...model, numThreads, provider: "cpu", debug: 0 },
    decodingMethod: "greedy_search",
  };
}

/** Every hotword sits in the model's prompt, which shares a fixed budget with the audio. */
const MAX_HOTWORDS = 60;
const MAX_HOTWORD_CHARS = 600;

/** Renderer-supplied hotwords as sherpa's comma-separated list. */
export function hotwordList(value: unknown): string {
  if (!Array.isArray(value)) return "";
  const words: string[] = [];
  let chars = 0;
  for (const raw of value) {
    if (typeof raw !== "string") continue;
    const word = raw.replace(/[,，\s]+/g, " ").trim().slice(0, 40);
    if (!word || chars + word.length > MAX_HOTWORD_CHARS) continue;
    words.push(word);
    chars += word.length + 1;
    if (words.length >= MAX_HOTWORDS) break;
  }
  return words.join(",");
}

const MAX_SEGMENT_SECONDS = 20;
const MIN_SEGMENT_SECONDS = 10;
const PAUSE_WINDOW_SECONDS = 0.3;

/**
 * Cut a long clip into pieces of at most 20 seconds, each ending at the
 * quietest stretch between 10 and 20 seconds in, so cuts fall between words.
 * Qwen3-ASR loses everything past ~40 seconds in one pass, and decode time
 * grows faster than the clip.
 */
export function splitAtPauses(samples: Float32Array, sampleRate: number): Float32Array[] {
  const max = MAX_SEGMENT_SECONDS * sampleRate;
  if (samples.length <= max) return [samples];
  const window = Math.round(PAUSE_WINDOW_SECONDS * sampleRate);
  const hop = Math.round(window / 3);
  const segments: Float32Array[] = [];
  let start = 0;
  while (samples.length - start > max) {
    let cut = start + max;
    let quietest = Infinity;
    // Latest first, moving earlier only for a clearly quieter stretch, so
    // evenly loud audio still gets the longest segments.
    for (let at = start + max - window; at >= start + MIN_SEGMENT_SECONDS * sampleRate; at -= hop) {
      let energy = 0;
      for (let i = at; i < at + window; i++) energy += samples[i]! * samples[i]!;
      if (energy < quietest / 2 || quietest === Infinity) {
        quietest = energy;
        cut = at + Math.round(window / 2);
      }
    }
    segments.push(samples.subarray(start, cut));
    start = cut;
  }
  segments.push(samples.subarray(start));
  return segments;
}

const CJK_EDGE = /[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uac00-\ud7af\uff00-\uffef]/;

/** Join segment transcripts: no space where either side is CJK. */
export function joinTranscripts(parts: readonly string[]): string {
  let out = "";
  for (const raw of parts) {
    const part = raw.trim();
    if (!part) continue;
    const spaced = out && !CJK_EDGE.test(out[out.length - 1]!) && !CJK_EDGE.test(part[0]!);
    out += (spaced ? " " : "") + part;
  }
  return out;
}
