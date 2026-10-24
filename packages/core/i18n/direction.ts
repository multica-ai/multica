export type TextDirection = "ltr" | "rtl";

const RTL_LANGUAGES = new Set(["ar", "fa", "he", "ur"]);

/** Accepts a BCP-47 tag, a POSIX-style tag or a resource key, e.g. `ar`, `ar-SA`, `ar_EG`, `zh-Hans`. */
export function getDir(locale: string): TextDirection {
  const language = locale.split(/[-_]/)[0]?.toLowerCase() ?? "";
  return RTL_LANGUAGES.has(language) ? "rtl" : "ltr";
}
