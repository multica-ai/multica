"use client";

import { ArrowRight, ArrowUpRight } from "@multica/ui/icons";
import { useWorkspacePaths } from "@multica/core/paths";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { useSearchStore } from "../search/search-store";
import type { HighlightAction } from "./releases";

/**
 * The call to action of one highlight. `onDone` runs after the action fires
 * so the surrounding panel can close.
 */
export function HighlightActionButton({ action, onDone }: { action: HighlightAction; onDone: () => void }) {
  const { t } = useT("layout");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  if (action.kind === "external") {
    return (
      <a
        href={action.href}
        target="_blank"
        rel="noopener noreferrer"
        onClick={onDone}
        className="inline-flex items-center gap-0.5 text-label font-medium text-brand hover:underline"
      >
        {t(($) => $.whats_new.action.learn_more)}
        <ArrowUpRight className="size-3.5" />
      </a>
    );
  }

  const run = () => {
    onDone();
    if (action.kind === "command-palette") useSearchStore.getState().setOpen(true);
    else navigation.push(action.to(paths));
  };
  return (
    <button
      type="button"
      onClick={run}
      className="inline-flex cursor-pointer items-center gap-0.5 text-label font-medium text-brand hover:underline"
    >
      {action.kind === "command-palette" ? t(($) => $.whats_new.action.try) : t(($) => $.whats_new.action.open)}
      <ArrowRight className="size-3.5" />
    </button>
  );
}
