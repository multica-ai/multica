"use client";

import { ArrowRight, ArrowUpRight } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useShortcut } from "@multica/core/shortcuts";
import { useWorkspacePaths } from "@multica/core/paths";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { useSearchStore } from "../search/search-store";
import type { HighlightAction } from "./releases";

/**
 * The call to action of one highlight. `onDone` runs after the action fires
 * so the surrounding surface can close and record the dismissal.
 */
export function HighlightActionButton({
  action,
  variant,
  onDone,
}: {
  action: HighlightAction;
  /** "button" in the update card, "link" in the history panel. */
  variant: "button" | "link";
  onDone: () => void;
}) {
  const { t } = useT("layout");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const searchShortcut = useShortcut("openSearch");

  if (action.kind === "external") {
    const label = t(($) => $.whats_new.action.learn_more);
    return variant === "button" ? (
      <Button
        variant="outline"
        size="sm"
        render={<a href={action.href} target="_blank" rel="noopener noreferrer" onClick={onDone} />}
      >
        {label}
        <ArrowUpRight className="text-faint-foreground" />
      </Button>
    ) : (
      <a
        href={action.href}
        target="_blank"
        rel="noopener noreferrer"
        onClick={onDone}
        className="inline-flex items-center gap-0.5 text-label font-medium text-brand hover:underline"
      >
        {label}
        <ArrowUpRight className="size-3.5" />
      </a>
    );
  }

  const run = () => {
    onDone();
    if (action.kind === "command-palette") useSearchStore.getState().setOpen(true);
    else navigation.push(action.to(paths));
  };
  const label =
    action.kind === "command-palette"
      ? t(($) => $.whats_new.action.try)
      : t(($) => $.whats_new.action.open);

  if (variant === "button") {
    return (
      <Button variant="outline" size="sm" onClick={run}>
        {label}
        {action.kind === "command-palette" && searchShortcut && (
          <ShortcutKeycaps shortcut={searchShortcut} decorative />
        )}
      </Button>
    );
  }
  return (
    <button
      type="button"
      onClick={run}
      className="inline-flex cursor-pointer items-center gap-0.5 text-label font-medium text-brand hover:underline"
    >
      {label}
      <ArrowRight className="size-3.5" />
    </button>
  );
}
