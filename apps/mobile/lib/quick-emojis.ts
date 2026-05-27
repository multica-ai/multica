/**
 * The eight curated quick-react emoji for the comment actions row.
 * Duplicated verbatim from web's QUICK_EMOJIS at
 * packages/ui/components/common/quick-emoji-picker.tsx — same set shown on
 * both clients so muscle memory carries between web and mobile.
 *
 * The actions sheet renders the first N (currently 5) in the inline row and
 * exposes the rest via the "More reactions" entry that pushes the formSheet
 * emoji-picker route.
 */
export const QUICK_EMOJIS = [
  "👍",
  "👌",
  "❤️",
  "✅",
  "🎉",
  "😕",
  "🚀",
  "👀",
] as const;
