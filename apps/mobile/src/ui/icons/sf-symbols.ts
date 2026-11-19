import type { IconName } from "./Icon";

/**
 * SF Symbols for icons that system bars draw. Bar items and tabs must be native to move with the
 * system, onto iPhone Duo's vertical bar for one, so they take symbols rather than our glyphs.
 */
const SYMBOLS: Partial<Record<IconName, string>> = {
  Bot: "cpu",
  CheckCircle2: "checkmark.circle",
  ChevronsUpDown: "chevron.up.chevron.down",
  CircleUser: "person.crop.circle",
  Inbox: "tray",
  MoreHorizontal: "ellipsis",
  Plus: "plus",
  Search: "magnifyingglass",
  Settings: "gearshape",
  SquarePen: "square.and.pencil",
  Star: "star",
  X: "xmark",
};

export function sfSymbol(icon: IconName, filled = false): string {
  const name = SYMBOLS[icon];
  if (!name) throw new Error(`No SF Symbol for icon "${icon}"; add one to sf-symbols.ts.`);
  return filled ? `${name}.fill` : name;
}
