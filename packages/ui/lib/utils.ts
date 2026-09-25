import { clsx, type ClassValue } from "clsx"
import { extendTailwindMerge } from "tailwind-merge"

/**
 * The type scale's steps are role-named (`text-body`, `text-caption`), so
 * tailwind-merge cannot tell them apart from a colour utility: `text-<x>` is
 * ambiguous, and its built-in table only lists Tailwind's default sizes. Left
 * unconfigured it files every role step under text-colour, and then drops
 * whichever of `text-body` / `text-muted-foreground` came first as a conflict —
 * silently, so an element renders at the inherited 16px or in the wrong colour
 * with nothing in the source to explain it.
 *
 * Registering the steps as font-size restores the real conflict groups: size
 * beats size, colour beats colour, and the two coexist. Keep this list in sync
 * with the `--text-*` steps in `packages/ui/styles/tokens.css`.
 */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [
        {
          text: [
            "micro",
            "caption",
            "label",
            "body",
            "body-lg",
            "title-sm",
            "title",
            "title-lg",
            "display-sm",
            "display",
          ],
        },
      ],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
