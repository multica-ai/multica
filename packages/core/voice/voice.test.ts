import { describe, expect, it } from "vitest";
import {
  DEFAULT_VOICE_SETTINGS,
  MAX_VOCABULARY_WORDS,
  dictationSeparator,
  removeSpokenFillers,
  sanitizeVoiceSettings,
  vocabularyPrompt,
} from ".";

describe("dictationSeparator", () => {
  it.each([
    ["", "hello", ""],
    ["Fix the", "badge", " "],
    ["Fix the ", "badge", ""],
    ["切换工作区以后", "角标不更新", ""],
    ["Design 工作区", "badge", ""],
    ["the badge", "不更新", ""],
    ["line\n", "next", ""],
  ])("between %j and %j", (before, text, sep) => {
    expect(dictationSeparator(before, text)).toBe(sep);
  });
});

describe("removeSpokenFillers", () => {
  it.each([
    ["嗯，我刚在 Design 工作区复现了", "我刚在 Design 工作区复现了"],
    ["我觉得，呃，应该是缓存", "我觉得，应该是缓存"],
    ["Um, I think uh it's the cache", "I think it's the cache"],
    ["the umbrella and the hmm", "the umbrella and the"],
    ["Uhh the drum", "the drum"],
  ])("%j", (input, output) => {
    expect(removeSpokenFillers(input)).toBe(output);
  });

  it("leaves meaningful words alone", () => {
    expect(removeSpokenFillers("嗯哼是一个词 human umami")).toBe("嗯哼是一个词 human umami");
  });
});

describe("vocabularyPrompt", () => {
  it("joins trimmed words", () => {
    expect(vocabularyPrompt([" Multica ", "", "Lambda"])).toBe("Multica, Lambda");
  });
});

describe("sanitizeVoiceSettings", () => {
  it("falls back to defaults for junk", () => {
    expect(sanitizeVoiceSettings(null)).toEqual(DEFAULT_VOICE_SETTINGS);
    expect(
      sanitizeVoiceSettings({ activation: "shout", language: "xx", deviceId: 3, removeFillers: "yes" }),
    ).toEqual(DEFAULT_VOICE_SETTINGS);
  });

  it("keeps valid values and dedupes the vocabulary", () => {
    const words = Array.from({ length: MAX_VOCABULARY_WORDS + 5 }, (_, i) => `w${i}`);
    const settings = sanitizeVoiceSettings({
      activation: "hold",
      language: "zh",
      deviceId: "mic-1",
      removeFillers: false,
      vocabulary: ["Multica", "multica", " ", 7, ...words],
    });
    expect(settings.activation).toBe("hold");
    expect(settings.language).toBe("zh");
    expect(settings.deviceId).toBe("mic-1");
    expect(settings.removeFillers).toBe(false);
    expect(settings.vocabulary[0]).toBe("Multica");
    expect(settings.vocabulary).toHaveLength(MAX_VOCABULARY_WORDS);
  });
});
