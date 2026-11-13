import { join } from "path";
import type { RecognizerKind } from "./catalog";

/** Which recognizer to have loaded; `key` changes whenever any field does. */
export interface LoadSpec {
  key: string;
  recognizer: RecognizerKind;
  dir: string;
  /** A VOICE_LANGUAGES value. */
  language: string;
}

export type WorkerRequest =
  | { id: number; type: "load"; spec: LoadSpec }
  | { id: number; type: "transcribe"; spec: LoadSpec; samples: Float32Array; sampleRate: number };

export type WorkerResponse = { id: number; ok: true; text?: string } | { id: number; ok: false; error: string };

/** sherpa-onnx's offline recognizer config for a model folder. */
export function recognizerConfig(spec: LoadSpec, numThreads: number): Record<string, unknown> {
  const file = (name: string) => join(spec.dir, name);
  const model =
    spec.recognizer === "sense-voice"
      ? {
          senseVoice: {
            model: file("model.int8.onnx"),
            language: spec.language === "auto" ? "" : spec.language,
            useInverseTextNormalization: 1,
          },
        }
      : {
          transducer: {
            encoder: file("encoder.int8.onnx"),
            decoder: file("decoder.int8.onnx"),
            joiner: file("joiner.int8.onnx"),
          },
          modelType: "nemo_transducer",
        };
  return {
    featConfig: { sampleRate: 16000, featureDim: 80 },
    modelConfig: { ...model, tokens: file("tokens.txt"), numThreads, provider: "cpu", debug: 0 },
    decodingMethod: "greedy_search",
  };
}
