export const LARGE_TEXT_THRESHOLD = 4_000;

export function shouldHighlightCode(
  code: string,
  language: string | null | undefined,
): language is string {
  return Boolean(language) && code.length <= LARGE_TEXT_THRESHOLD;
}
