import { pinyin } from "pinyin-pro";

/**
 * Check if a query matches a name via pinyin.
 * Supports:
 * - Full pinyin match: "liyunlong" matches "李云龙"
 * - Initial letter abbreviation: "lyl" matches "李云龙"
 * - Partial prefix match: "liyu" matches "李云龙"
 */
export function matchesPinyin(name: string, query: string): boolean {
  if (!query) return true;

  // Only attempt pinyin matching if the name contains Chinese characters
  if (!/[\u4e00-\u9fff]/.test(name)) return false;

  const q = query.toLowerCase();

  // Get full pinyin (no tone, no separator, ü→v for standard input)
  const full = pinyin(name, { toneType: "none", type: "array", v: true });
  const fullStr = full.join("");

  // Full pinyin prefix match: "liyunlong" or "liyun"
  if (fullStr.startsWith(q)) return true;

  // Initial letters match: "lyl"
  const initials = full.map((p) => p[0] || "").join("");
  if (initials.startsWith(q)) return true;

  // Hybrid match: some chars matched by full pinyin, rest by initials
  // e.g. "liyl" matches "李云龙" (li + y + l)
  return hybridMatch(full, q);
}

/**
 * Hybrid matching: the query can be a mix of full pinyin for some characters
 * and initials for others, consumed left-to-right.
 * e.g. for ["li", "yun", "long"], query "liyunl" matches (li + yun + l)
 */
function hybridMatch(pinyinArr: string[], query: string): boolean {
  return match(pinyinArr, 0, query, 0);
}

function match(arr: string[], ai: number, q: string, qi: number): boolean {
  if (qi >= q.length) return true;
  if (ai >= arr.length) return false;

  const syllable = arr[ai]!;

  // Try matching full syllable or any prefix of it
  for (let len = syllable.length; len >= 1; len--) {
    if (qi + len > q.length) continue;
    if (q.substring(qi, qi + len) === syllable.substring(0, len)) {
      if (match(arr, ai + 1, q, qi + len)) return true;
    }
  }

  return false;
}
