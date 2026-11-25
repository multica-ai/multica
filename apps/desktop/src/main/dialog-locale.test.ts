import { afterEach, describe, expect, it } from "vitest";
import {
  getEffectiveDialogLocale,
  resolveDialogLocale,
  setAppDialogLocale,
} from "./dialog-locale";

describe("resolveDialogLocale", () => {
  afterEach(() => {
    setAppDialogLocale(null);
  });

  it("prefers the renderer's in-app locale override over the OS system locale", () => {
    expect(getEffectiveDialogLocale(() => "zh-CN")).toBe("zh-CN");
    setAppDialogLocale("en");
    expect(getEffectiveDialogLocale(() => "zh-CN")).toBe("en");
    expect(resolveDialogLocale(getEffectiveDialogLocale(() => "zh-CN"))).toBe("en");
    setAppDialogLocale(null);
    expect(getEffectiveDialogLocale(() => "zh-CN")).toBe("zh-CN");
  });
  it("parses exact BCP-47 language subtags without prefix collisions", () => {
    expect(resolveDialogLocale("ko")).toBe("ko");
    expect(resolveDialogLocale("ko-KR")).toBe("ko");
    expect(resolveDialogLocale("kok")).toBe("en");
    expect(resolveDialogLocale("kok-IN")).toBe("en");
    expect(resolveDialogLocale("ja-JP")).toBe("ja");
    expect(resolveDialogLocale("fr-CA")).toBe("fr");
    expect(resolveDialogLocale("es-MX")).toBe("es");
    expect(resolveDialogLocale("ru-RU")).toBe("ru");
  });

  it("distinguishes Simplified and Traditional Chinese across hyphen and underscore separators", () => {
    expect(resolveDialogLocale("zh-CN")).toBe("zh-Hans");
    expect(resolveDialogLocale("zh_CN")).toBe("zh-Hans");
    expect(resolveDialogLocale("zh-Hans-SG")).toBe("zh-Hans");
    expect(resolveDialogLocale("zh-TW")).toBe("zh-Hant");
    expect(resolveDialogLocale("zh_TW")).toBe("zh-Hant");
    expect(resolveDialogLocale("zh-Hant-HK")).toBe("zh-Hant");
    expect(resolveDialogLocale("zh_MO")).toBe("zh-Hant");
  });

  it("falls back to English for unknown or empty locales", () => {
    expect(resolveDialogLocale("")).toBe("en");
    expect(resolveDialogLocale("de-DE")).toBe("en");
  });
});
