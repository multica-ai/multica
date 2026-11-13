/**
 * Speech models Desktop offers for download. Files are pinned to a Hugging
 * Face commit and checked against their size and SHA-256, so a mirror can
 * serve them without being trusted.
 */

export interface CatalogFile {
  name: string;
  size: number;
  sha256: string;
}

export type RecognizerKind = "sense-voice" | "nemo-transducer";

export interface CatalogModel {
  id: string;
  name: string;
  author: string;
  licenseUrl: string;
  repo: string;
  revision: string;
  languages: string[];
  languageHint: boolean;
  recognizer: RecognizerKind;
  files: CatalogFile[];
}

/** Tried in order; each serves `{base}/{repo}/resolve/{revision}/{file}`. */
export const MODEL_MIRRORS = ["https://huggingface.co", "https://hf-mirror.com"];

export const VOICE_MODELS: readonly CatalogModel[] = [
  {
    id: "sensevoice-small",
    name: "SenseVoice Small",
    author: "Alibaba FunAudioLLM",
    licenseUrl: "https://github.com/modelscope/FunASR/blob/main/MODEL_LICENSE",
    repo: "csukuangfj/sherpa-onnx-sense-voice-zh-en-ja-ko-yue-2024-07-17",
    revision: "2365baeacb507f821a0c8120fcee3d484dba7a07",
    languages: ["zh", "en", "ja", "ko", "yue"],
    languageHint: true,
    recognizer: "sense-voice",
    files: [
      {
        name: "model.int8.onnx",
        size: 239233841,
        sha256: "c71f0ce00bec95b07744e116345e33d8cbbe08cef896382cf907bf4b51a2cd51",
      },
      {
        name: "tokens.txt",
        size: 315894,
        sha256: "f449eb28dc567533d7fa59be34e2abca8784f771850c78a47fb731a31429a1dc",
      },
    ],
  },
  {
    id: "parakeet-tdt-0.6b-v3",
    name: "Parakeet V3",
    author: "NVIDIA",
    licenseUrl: "https://creativecommons.org/licenses/by/4.0/",
    repo: "csukuangfj/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8",
    revision: "ed5d745623472f97cfc2e6c2916ee248798fc5a8",
    languages: [
      "en", "de", "es", "fr", "it", "pt", "nl", "ru", "uk", "pl", "cs", "sk", "sl",
      "hr", "bg", "ro", "hu", "el", "sv", "da", "fi", "et", "lv", "lt", "mt",
    ],
    languageHint: false,
    recognizer: "nemo-transducer",
    files: [
      {
        name: "encoder.int8.onnx",
        size: 652282294,
        sha256: "1afbe69c1ffdabee94755f42e4daf1d93549131a3da11356bda84fcef0f2c479",
      },
      {
        name: "decoder.int8.onnx",
        size: 11845274,
        sha256: "0222a86147299d4dee90edf95b6d13827edb42ad8b7c9cf0b2582f030937c0b5",
      },
      {
        name: "joiner.int8.onnx",
        size: 6355277,
        sha256: "cbd02f3696e9944970111fb439d9c3fd365a80374565f13f2b9c7007d253fc98",
      },
      {
        name: "tokens.txt",
        size: 93939,
        sha256: "d58544679ea4bc6ac563d1f545eb7d474bd6cfa467f0a6e2c1dc1c7d37e3c35d",
      },
    ],
  },
];

export function findVoiceModel(id: string): CatalogModel | undefined {
  return VOICE_MODELS.find((model) => model.id === id);
}

export function modelSize(model: CatalogModel): number {
  return model.files.reduce((sum, file) => sum + file.size, 0);
}
