"use client";

import type { ReactNode } from "react";
import { createShortcutChord } from "@multica/core/shortcuts";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useT } from "../i18n";

const UP = createShortcutChord("Up");
const DOWN = createShortcutChord("Down");
const ENTER = createShortcutChord("Enter");
const OPEN_IN_TAB = createShortcutChord("Enter", { primary: true });
const SHIFT_ENTER = createShortcutChord("Enter", { shift: true });
const BACKSPACE = createShortcutChord("Backspace");

function Hint({ keys, children }: { keys: ReactNode; children: ReactNode }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5">
      {keys}
      <span>{children}</span>
    </span>
  );
}

function PrefixHint({ prefix, children }: { prefix: string; children: ReactNode }) {
  return (
    <Hint
      keys={
        <Kbd aria-hidden="true" className="h-5 min-w-5 border border-border/70 bg-muted/80 px-1 text-micro">
          {prefix}
        </Kbd>
      }
    >
      {children}
    </Hint>
  );
}

/**
 * Always-visible key hints. The left side says what the keys do right now;
 * the right side teaches the scope prefixes (or how to go back).
 */
export function PaletteFooter({
  mode,
  delegateName,
}: {
  mode: "root" | "page" | "delegate";
  delegateName?: string;
}) {
  const { t } = useT("search");
  return (
    <div className="flex items-center gap-4 border-t px-3 py-2 text-caption text-muted-foreground">
      <div className="flex min-w-0 items-center gap-4 overflow-hidden">
        {mode === "delegate" ? (
          <>
            <Hint keys={<ShortcutKeycaps shortcut={ENTER} decorative />}>
              {t(($) => $.footer.delegate, { name: delegateName ?? "" })}
            </Hint>
            <Hint keys={<ShortcutKeycaps shortcut={SHIFT_ENTER} decorative />}>
              {t(($) => $.footer.search_instead)}
            </Hint>
          </>
        ) : (
          <>
            <Hint
              keys={
                <span className="inline-flex gap-0.5">
                  <ShortcutKeycaps shortcut={UP} decorative />
                  <ShortcutKeycaps shortcut={DOWN} decorative />
                </span>
              }
            >
              {t(($) => $.footer.navigate)}
            </Hint>
            <Hint keys={<ShortcutKeycaps shortcut={ENTER} decorative />}>{t(($) => $.footer.select)}</Hint>
            {mode === "root" && (
              <Hint keys={<ShortcutKeycaps shortcut={OPEN_IN_TAB} decorative />}>
                {t(($) => $.footer.open_in_new_tab)}
              </Hint>
            )}
          </>
        )}
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-4">
        {mode === "page" ? (
          <Hint keys={<ShortcutKeycaps shortcut={BACKSPACE} decorative />}>{t(($) => $.footer.back)}</Hint>
        ) : (
          <>
            <PrefixHint prefix=">">{t(($) => $.footer.prefix_commands)}</PrefixHint>
            <PrefixHint prefix="#">{t(($) => $.footer.prefix_issues)}</PrefixHint>
            <PrefixHint prefix="@">{t(($) => $.footer.prefix_people)}</PrefixHint>
          </>
        )}
      </div>
    </div>
  );
}
