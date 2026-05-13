import type { ResolvedPos } from "@tiptap/pm/model";
import type { SuggestionMatch } from "@tiptap/suggestion";

/**
 * Custom matcher for the `/skill` trigger.
 *
 * Why not `char: "/skill"` on the default matcher? @tiptap/suggestion's
 * default builds the regex `\/skill[^\s\/skill]*`, whose character class
 * excludes `/`, `s`, `k`, `i`, `l` — so any query containing those letters
 * (which is most real skill names) terminates the match prematurely.
 *
 * Rules enforced here:
 * - `/skill` must start the text node OR be preceded by whitespace, so it
 *   doesn't fire inside other tokens (e.g. `apps/skills/foo`).
 * - Case-insensitive trigger (`/Skill`, `/SKILL` all work).
 * - The trigger requires a word-boundary: typing `/skills` (extra `s`) does
 *   NOT match — `$` only anchors when nothing else follows `/skill` besides
 *   an optional whitespace + query.
 * - Query starts after the first whitespace following `/skill`; leading
 *   whitespace is trimmed (so `/skill   foo` matches `foo`).
 */
const SKILL_TRIGGER = /(^|\s)(\/skill)(?:\s(.*))?$/i;

export function findSkillSuggestionMatch(config: {
  $position: ResolvedPos;
}): SuggestionMatch | null {
  const nodeBefore = config.$position.nodeBefore;
  if (!nodeBefore?.isText || !nodeBefore.text) return null;
  const text = nodeBefore.text;

  const match = SKILL_TRIGGER.exec(text);
  if (!match) return null;

  const leadingWs = match[1] ?? "";
  const trigger = match[2] ?? "";
  const queryPart = match[3];

  const matchIndex = (match.index ?? 0) + leadingWs.length;
  const textFrom = config.$position.before() + 1;
  const from = textFrom + matchIndex;
  const to = textFrom + text.length;

  const query = (queryPart ?? "").trimStart();
  const matchedText = trigger + (queryPart === undefined ? "" : ` ${queryPart}`);

  return {
    range: { from, to },
    query,
    text: matchedText,
  };
}
