// Stub for the pinyin-match module. The full implementation (upstream
// #2572 / #2582 / #2584) wires Chinese pinyin transliteration into name
// search so a user can find `独立团` by typing `dulituan`. The squad PRs
// in this batch import it, but the pinyin batch hasn't landed in this fork
// yet — so the lookup always returns false. Replace this stub by cherry-
// picking c628958f / d1c8c213 / 053a37d1 (plus the `pinyin` npm dep).
export function matchesPinyin(_name: string, _query: string): boolean {
  return false;
}
