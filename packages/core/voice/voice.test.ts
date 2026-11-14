import { describe, expect, it } from "vitest";
import {
  DEFAULT_VOICE_SETTINGS,
  MAX_VOCABULARY_WORDS,
  applyVocabulary,
  dictationSeparator,
  removeSpokenFillers,
  sanitizeVoiceSettings,
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
    ["brave", "world", " "],
    ["brave", ", then", ""],
    ["brave", "'s", ""],
    ["see (", "docs", ""],
    ["缓存", "first", ""],
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
    // Glued to the words around them, as the model writes them mid-sentence.
    ["还有一个场景，就是呃，annotation的功能", "还有一个场景，就是 annotation的功能"],
    ["转写的那个呃标识会和呃 reply 左下角的", "转写的那个标识会和 reply 左下角的"],
    ["will work 的那个呃呃，就是重叠在一起。", "will work 的那个就是重叠在一起。"],
    ["我嗯觉得可以", "我觉得可以"],
    ["那个呃。", "那个。"],
  ])("%j", (input, output) => {
    expect(removeSpokenFillers(input)).toBe(output);
  });

  it("leaves meaningful words alone", () => {
    expect(removeSpokenFillers("嗯哼是一个词 human umami")).toBe("嗯哼是一个词 human umami");
    expect(removeSpokenFillers("金额，额外的额度和呃逆")).toBe("金额，额外的额度和呃逆");
  });
});

describe("applyVocabulary", () => {
  it.each([
    ["assign it to lambda in multica", "assign it to Lambda in Multica"],
    ["open the git hub link", "open the GitHub link"],
    ["Git-Hub and github", "GitHub and GitHub"],
    ["lambdas and multicast stay", "lambdas and multicast stay"],
    ["交给 lambda 处理", "交给 Lambda 处理"],
  ])("%j", (input, output) => {
    expect(applyVocabulary(input, [" Multica ", "", "Lambda", "GitHub", "工作区"])).toBe(output);
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
      modelId: "sensevoice-small",
    });
    expect(settings.activation).toBe("hold");
    expect(settings.language).toBe("zh");
    expect(settings.deviceId).toBe("mic-1");
    expect(settings.removeFillers).toBe(false);
    expect(settings.vocabulary[0]).toBe("Multica");
    expect(settings.vocabulary).toHaveLength(MAX_VOCABULARY_WORDS);
    expect(settings.modelId).toBe("sensevoice-small");
  });
});
