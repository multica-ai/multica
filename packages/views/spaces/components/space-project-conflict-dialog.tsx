"use client";

import { Check } from "lucide-react";
import type { Space } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { SpaceIcon } from "./space-icon";
import { useT } from "../../i18n";

/**
 * Resolution dialog for "the issue's space is not part of the selected
 * project" (Linear-style). The first option mirrors the server-side default
 * for headless callers — adding the space to the project — so confirming it
 * simply proceeds; picking a project space instead retargets the issue.
 */
export function SpaceProjectConflictDialog({
  open,
  spaceName,
  projectName,
  projectSpaces,
  onAddSpace,
  onMoveToSpace,
  onCancel,
}: {
  open: boolean;
  spaceName: string;
  projectName: string;
  /** The project's current spaces, offered as "move the issue there" targets. */
  projectSpaces: Space[];
  onAddSpace: () => void;
  onMoveToSpace: (spaceId: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("spaces");
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <DialogContent showCloseButton={false} className="sm:max-w-md">
        <DialogTitle>{t(($) => $.conflict.title, { space: spaceName })}</DialogTitle>
        <DialogDescription>
          {t(($) => $.conflict.body, { space: spaceName, project: projectName })}
        </DialogDescription>
        <div className="flex flex-col gap-1">
          <button
            type="button"
            autoFocus
            onClick={onAddSpace}
            className="flex items-center gap-2 rounded-md border border-input bg-accent/40 px-3 py-2 text-left text-sm transition-colors hover:bg-accent"
          >
            <span className="min-w-0 flex-1 truncate">
              {t(($) => $.conflict.add_space, { space: spaceName })}
            </span>
            <Check className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          </button>
          {projectSpaces.length > 0 && (
            <>
              <div className="px-1 pt-1 text-xs text-muted-foreground">
                {t(($) => $.conflict.project_spaces)}
              </div>
              {projectSpaces.map((space) => (
                <button
                  key={space.id}
                  type="button"
                  onClick={() => onMoveToSpace(space.id)}
                  className="flex items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-accent/60"
                >
                  <SpaceIcon space={space} />
                  <span className="min-w-0 flex-1 truncate">
                    {t(($) => $.conflict.move_issue, { space: space.name })}
                  </span>
                </button>
              ))}
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
