// @vitest-environment node
import { describe, expect, it } from "vitest";
import { hotwordList, joinTranscripts, recognizerConfig, splitAtPauses, type LoadSpec } from "./worker-protocol";

const RATE = 16000;

/** Loud noise with quiet stretches centred at the given seconds. */
function speech(seconds: number, pausesAt: number[]): Float32Array {
  const samples = new Float32Array(seconds * RATE);
  for (let i = 0; i < samples.length; i++) samples[i] = Math.sin(i * 0.3) * 0.5;
  for (const at of pausesAt) samples.fill(0, (at - 0.2) * RATE, (at + 0.2) * RATE);
  return samples;
}

describe("splitAtPauses", () => {
  it("keeps a short clip whole", () => {
    const samples = speech(12, []);
    expect(splitAtPauses(samples, RATE)).toEqual([samples]);
  });

  it("cuts long clips at pauses into pieces of at most 20 seconds", () => {
    const samples = speech(50, [14, 31]);
    const segments = splitAtPauses(samples, RATE);
    expect(segments.map((s) => Math.round(s.length / RATE))).toEqual([14, 17, 19]);
    expect(segments.reduce((sum, s) => sum + s.length, 0)).toBe(samples.length);
  });

  it("cuts at 20 seconds when there is no pause", () => {
    const segments = splitAtPauses(speech(45, []), RATE);
    expect(segments.every((s) => s.length <= 20 * RATE)).toBe(true);
    expect(segments.length).toBe(3);
  });
});

describe("joinTranscripts", () => {
  it("spaces Latin text and runs CJK together", () => {
    expect(joinTranscripts(["Assign it to Lambda.", " Then ship it. "])).toBe("Assign it to Lambda. Then ship it.");
    expect(joinTranscripts(["把这个交给", "Lambda", "处理。", ""])).toBe("把这个交给Lambda处理。");
  });
});

describe("hotwordList", () => {
  it("strips separators and ignores non-strings", () => {
    expect(hotwordList(["Multica", "a,b", "张，三", 3, "  "])).toBe("Multica,a b,张 三");
    expect(hotwordList("Multica")).toBe("");
  });
});

describe("recognizerConfig", () => {
  it("points Qwen3-ASR at its files and passes hotwords", () => {
    const spec: LoadSpec = { key: "k", recognizer: "qwen3-asr", dir: "/m", language: "auto", hotwords: "Multica,Lambda" };
    const config = recognizerConfig(spec, 4) as { modelConfig: Record<string, unknown> };
    expect(config.modelConfig).toMatchObject({
      qwen3Asr: {
        convFrontend: "/m/conv_frontend.onnx",
        encoder: "/m/encoder.int8.onnx",
        decoder: "/m/decoder.int8.onnx",
        tokenizer: "/m/tokenizer",
        hotwords: "Multica,Lambda",
      },
      tokens: "",
      numThreads: 4,
    });
  });
});
