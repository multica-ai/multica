import { pinyin } from "pinyin-pro";
import type { SupportedLocale } from "@multica/core/i18n";
import { CELESTIAL_WORKSPACE_NAMES } from "./celestial-workspace-names";

export const WORKSPACE_SLUG_REGEX = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

const WORKSPACE_SLUG_SUFFIX_ALPHABET = "abcdefghijklmnopqrstuvwxyz0123456789";
const WORKSPACE_SLUG_SUFFIX_LENGTH = 4;

/** Maximal runs of Han characters, including the extension-A and compatibility blocks. */
const HAN_RUN = /[㐀-䶿一-鿿豈-﫿]+/g;

/** Hiragana / katakana, including the halfwidth katakana block. */
const KANA = /[\u3041-\u3096\u309B-\u30FF\uFF66-\uFF9F]/;

/**
 * Han characters are shared, their readings are not: 東京 is "dongjing" in
 * Mandarin and "tokyo" in Japanese, and pinyin has nothing useful to say
 * about a Japanese or Korean name. Skip romanization when the text or the
 * reader's own language says the name isn't Chinese — those names fall back
 * to the empty slug the form already handles, which is a blank field the
 * user fills in rather than a wrong reading they have to notice and undo.
 *
 * Kana is a certain signal and works in any UI language; the locale catches
 * all-kanji names, which carry no signal of their own. Neither sees a
 * Japanese all-kanji name typed into a non-Japanese UI — that one still
 * romanizes, and fixing it needs real Japanese romaji (a segmenter plus a
 * reading dictionary), not another heuristic.
 */
function shouldRomanize(name: string, locale?: SupportedLocale): boolean {
  if (locale === "ja" || locale === "ko") return false;
  return !KANA.test(name);
}

/**
 * Romanize Han characters so a Chinese name can produce a slug at all.
 *
 * Each run is converted whole rather than per character: pinyin-pro resolves
 * 多音字 from its phrase dictionary, so 长沙 is "changsha" (not "zhangsha")
 * and 重庆 is "chongqing" (not "zhongqing") — but only when it can see the
 * surrounding characters. Syllables are joined without separators ("蜘蛛侠"
 * → "zhizhuxia") because one word should read as one slug segment; the
 * surrounding spaces keep a run from gluing onto adjacent Latin text.
 */
function romanizeHan(name: string): string {
  return name.replace(HAN_RUN, (run) => {
    const syllables = pinyin(run, { toneType: "none", type: "array", v: true });
    return ` ${syllables.join("")} `;
  });
}

/**
 * Auto-generate a slug from a workspace name.
 *
 * Chinese names are romanized first (蜘蛛侠 → "zhizhuxia"), so the create
 * form can fill in a URL — and, through it, an issue prefix — for a name
 * with no Latin characters at all (MUL-6050). This is a derived default the
 * user can still edit before creating, not a hardcoded one: the objection to
 * a fixed fallback like "workspace" was that it picks a useless URL and
 * collides for the second such workspace on the instance, and neither is
 * true of a romanized name.
 *
 * Only Chinese names romanize: `shouldRomanize` opts out of Japanese and
 * Korean, whose Han characters read differently. Pass the reader's `locale`
 * wherever it is known.
 *
 * Still returns empty string for names that romanize to nothing — kana-only,
 * Hangul-only, emoji-only, or any name opted out above — where the form
 * leaves the field empty and asks the user to type one.
 */
export function nameToWorkspaceSlug(
  name: string,
  locale?: SupportedLocale,
): string {
  const romanized = shouldRomanize(name, locale) ? romanizeHan(name) : name;
  return romanized
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

/**
 * Generates a localized workspace name while keeping its URL based on the
 * stable English slug. The server's unique slug constraint remains the final
 * authority if two users ever generate the same candidate.
 */
export function randomCelestialWorkspaceIdentity(
  locale: SupportedLocale,
  random: () => number = Math.random,
): { name: string; slug: string } {
  const celestial =
    CELESTIAL_WORKSPACE_NAMES[
      Math.floor(random() * CELESTIAL_WORKSPACE_NAMES.length)
    ]!;
  let suffix = "";

  for (let i = 0; i < WORKSPACE_SLUG_SUFFIX_LENGTH; i += 1) {
    suffix +=
      WORKSPACE_SLUG_SUFFIX_ALPHABET[
        Math.floor(random() * WORKSPACE_SLUG_SUFFIX_ALPHABET.length)
      ]!;
  }

  return {
    name: celestial.names[locale],
    slug: `${celestial.slugBase}-${suffix}`,
  };
}

export function isWorkspaceSlugConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    (error as { status?: unknown }).status === 409
  );
}
