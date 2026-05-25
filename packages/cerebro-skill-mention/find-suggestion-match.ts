import type { ResolvedPos } from "@tiptap/pm/model";
import type { SuggestionMatch } from "@tiptap/suggestion";

/**
 * Custom matcher for the `/` slash-command trigger.
 *
 * Why not the default matcher with `char: "/"`? @tiptap/suggestion's default
 * builds the regex `\/[^\s\/]*` but does not enforce the leading word-boundary
 * we need (so `apps/skills` would trigger). Doing it ourselves also gives a
 * single place to evolve the rules.
 *
 * Rules enforced here:
 * - `/` must start the text node OR be preceded by whitespace, so it doesn't
 *   fire inside paths or URLs (e.g. `apps/foo`, `https://x`).
 * - Query is whatever follows `/` up to the cursor; it stops at whitespace
 *   or another `/`. Typing a space closes the popover (Slack/Notion UX).
 * - Empty query is valid — bare `/` opens the menu with the full skill list.
 */
const SKILL_TRIGGER = /(^|\s)(\/)([^\s\/]*)$/;

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
  const queryPart = match[3] ?? "";

  const matchIndex = (match.index ?? 0) + leadingWs.length;
  const textFrom = config.$position.before() + 1;
  const from = textFrom + matchIndex;
  const to = textFrom + text.length;

  return {
    range: { from, to },
    query: queryPart,
    text: trigger + queryPart,
  };
}
